// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The window chrome's wiring: the main window, its saved place, theme, zoom,
//! full screen, and the small native calls (folder chooser, clipboard, opening
//! a file, the emoji keyboard).
//!
//! Like `lib.rs` this only connects Tauri to rules that live, and are tested,
//! in `agentrq-desktop-core` and `chrome_state`, so it is outside the coverage
//! gate. Each function names the Electron code it stands in for.

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use agentrq_desktop_core::app_icon::needs_window_icon;
use agentrq_desktop_core::emoji_panel::{show_emoji_panel, EmojiPanelHost};
use agentrq_desktop_core::files::{open_file_link, FileShell};
use agentrq_desktop_core::normal_bounds::{seed, to_logical, NormalBounds, Shown};
use agentrq_desktop_core::side_panel::child::is_guest_label;
use agentrq_desktop_core::theme::{
    apply_theme, background_color_for, rgb_from_hex, NativeTheme, ThemeSource, ThemedWindow,
};
use agentrq_desktop_core::window_state::{
    capture_window_state, clamp_to_displays, debounce, Bounds, Rect, StdTimers, WindowStateStore,
    WINDOW_STATE_FILENAME,
};
use agentrq_desktop_core::window_view::{
    toggle_full_screen, view_state, zoom_window, MaximizeWindow, ViewWindow, ZoomDirection,
};
use serde_json::Value;
use tauri::window::Color;
use tauri::{
    AppHandle, Manager, RunEvent, Runtime, Theme, Url, Webview, WebviewUrl, WebviewWindow,
    WebviewWindowBuilder, Window, WindowEvent,
};
use tauri_plugin_clipboard_manager::ClipboardExt;
use tauri_plugin_dialog::DialogExt;

use crate::chrome_state::{chooser_start, StateFile, ZoomTable};
use crate::profiles::{app_origin, Profiles};

pub const MAIN: &str = "main";

/// The main window's own webview. Never `get_webview_window(MAIN)`: Tauri
/// counts a window as a webview window only while every webview in it shares
/// its label, so that is `None` whenever the side panel's guest is open.
pub fn main_webview<R: Runtime>(app: &AppHandle<R>) -> Option<Webview<R>> {
    app.get_webview(MAIN)
}

/// What the chrome keeps for the whole app.
pub struct Chrome {
    store: WindowStateStore<StateFile>,
    zoom: ZoomTable,
    /// `currentTheme` in `index.js`: what the renderer last asked for.
    theme: Mutex<Option<String>>,
    /// Not on macOS, where wry reports no crash to guard.
    #[cfg(not(target_os = "macos"))]
    reloads: agentrq_desktop_core::webview_crash::ReloadGuard,
    /// Where the main window last sat un-maximized, which the quit-time save
    /// needs as much as the window's own handlers do.
    normal: Mutex<Arc<NormalBounds>>,
}

impl Chrome {
    pub fn new(data_dir: PathBuf) -> Self {
        Self {
            store: WindowStateStore::new(StateFile(data_dir.join(WINDOW_STATE_FILENAME))),
            zoom: ZoomTable::default(),
            theme: Mutex::new(None),
            #[cfg(not(target_os = "macos"))]
            reloads: Default::default(),
            normal: Mutex::new(Arc::new(NormalBounds::default())),
        }
    }
}

/// `process.platform`'s spelling, which `core` speaks.
#[cfg(target_os = "macos")]
const PLATFORM: &str = "darwin";
#[cfg(windows)]
const PLATFORM: &str = "win32";
#[cfg(not(any(target_os = "macos", windows)))]
const PLATFORM: &str = "linux";

/// The work areas of the attached displays, in logical pixels.
fn work_areas<R: Runtime>(app: &AppHandle<R>) -> Vec<Rect> {
    app.available_monitors()
        .unwrap_or_default()
        .iter()
        .map(|monitor| {
            let area = monitor.work_area();
            to_logical(
                Rect {
                    x: f64::from(area.position.x),
                    y: f64::from(area.position.y),
                    width: f64::from(area.size.width),
                    height: f64::from(area.size.height),
                },
                monitor.scale_factor(),
            )
        })
        .collect()
}

/// Where the window is, in logical pixels: its outer position and inner size,
/// the two a window builder takes back.
fn bounds_of<R: Runtime>(window: &Window<R>) -> Option<Rect> {
    let scale = window.scale_factor().ok()?;
    let position = window.outer_position().ok()?;
    let size = window.inner_size().ok()?;
    Some(to_logical(
        Rect {
            x: f64::from(position.x),
            y: f64::from(position.y),
            width: f64::from(size.width),
            height: f64::from(size.height),
        },
        scale,
    ))
}

/// `captureWindowState` and `save`, for the window as it is now.
fn save_state<R: Runtime>(app: &AppHandle<R>, window: &Window<R>, normal: &NormalBounds) {
    let Some(bounds) = bounds_of(window) else {
        return;
    };
    let seen = normal.look(
        bounds,
        Shown {
            maximized: window.is_maximized().unwrap_or(false),
            full_screen: window.is_fullscreen().unwrap_or(false),
            minimized: window.is_minimized().unwrap_or(false),
        },
    );
    let _ = app
        .state::<Chrome>()
        .store
        .save(&capture_window_state(&seen));
}

fn color_of(hex: &str) -> Option<Color> {
    rgb_from_hex(hex).map(|(r, g, b)| Color(r, g, b, 255))
}

/// A profile's webviews get its storage when they are built: a folder on Linux
/// and Windows, a WKWebView store on macOS (14 and later; below that the
/// profile's own origin separates it).
pub fn isolate<'a, R: Runtime>(
    builder: WebviewWindowBuilder<'a, R, AppHandle<R>>,
    profiles: &Profiles,
    profile: &str,
) -> WebviewWindowBuilder<'a, R, AppHandle<R>> {
    #[cfg(target_os = "macos")]
    let _ = profiles;
    #[cfg(target_os = "macos")]
    return builder.data_store_identifier(agentrq_desktop_core::profiles::data_store_id(profile));
    #[cfg(not(target_os = "macos"))]
    builder.data_directory(profiles.webview_dir(profile))
}

/// The main window on the active profile, from a Dock click or a profile
/// switch alike.
pub fn open_active_main_window<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let profile = app.state::<Arc<Profiles>>().active_id();
    open_main_window(app, &profile)
}

/// `createWindow`: the saved place, clamped to the displays; hidden, painted in
/// the theme's own background, and shown once the page has loaded, so there is
/// no white flash. On `profile`'s storage and origin, the only window that is
/// `main`; one builder, since Tauri keeps only the last `on_navigation` and
/// `on_page_load` it is given.
///
/// Built inside the run loop's `Destroyed` for the old one, so it must not
/// `listen` for anything aimed at `main`: Tauri drops those listeners after
/// that callback returns.
pub fn open_main_window<R: Runtime>(app: &AppHandle<R>, profile: &str) -> tauri::Result<()> {
    let chrome = app.state::<Chrome>();
    // A window made again starts at 100%, whatever the last one was at.
    chrome.zoom.forget(MAIN);
    let restored = chrome.store.load();
    let placement = clamp_to_displays(
        Some(Bounds {
            x: restored.x,
            y: restored.y,
            width: Some(restored.width),
            height: Some(restored.height),
        }),
        &work_areas(app),
    );
    let theme = chrome.theme.lock().unwrap().clone();
    // The system's own preference is not known before there is a window, so a
    // window made under "system" starts light and is repainted before it shows.
    let background = color_of(background_color_for(theme.as_deref(), false)).expect("static");

    let profiles = app.state::<Arc<Profiles>>().inner().clone();
    let origin = app_origin(profile);
    let url = Url::parse(&format!("{origin}/")).expect("a profile id is a host");
    let navigating = app.clone();

    let shown = Arc::new(AtomicBool::new(false));
    let on_load = Arc::clone(&shown);
    let win_app = app.clone();
    let mut builder = WebviewWindowBuilder::new(app, MAIN, WebviewUrl::CustomProtocol(url))
        .title("AgentRQ Tauri")
        .inner_size(placement.width, placement.height)
        .min_inner_size(
            agentrq_desktop_core::window_state::MIN_WIDTH,
            agentrq_desktop_core::window_state::MIN_HEIGHT,
        )
        .visible(false)
        .background_color(background)
        .on_page_load(move |window, payload| {
            main_page_load(window.app_handle(), &payload.event());
            if let tauri::webview::PageLoadEvent::Finished = payload.event() {
                reveal(&window, &on_load);
            }
        })
        .on_navigation(move |url| crate::shell::dispatch_navigation(&navigating, url))
        .on_new_window(move |url, _features| crate::shell::dispatch_new_window(&win_app, &url));
    // CI's harness (`test/e2e/theme-seed.js`): a script for the page to run
    // first. Only a debug build or the `e2e` feature reads it: a release
    // would run whatever a launcher's environment holds in the app's webview.
    #[cfg(any(debug_assertions, feature = "e2e"))]
    if let Ok(script) = std::env::var("AGENTRQ_E2E_INIT_SCRIPT") {
        builder = builder.initialization_script(script);
    }
    builder = match (placement.x, placement.y) {
        (Some(x), Some(y)) => builder.position(x, y),
        _ => builder.center(),
    };
    #[cfg(target_os = "macos")]
    {
        builder = builder
            .title_bar_style(tauri::TitleBarStyle::Overlay)
            .hidden_title(true)
            .traffic_light_position(tauri::LogicalPosition::new(16.0, 18.0));
    }
    if needs_window_icon(PLATFORM) {
        builder = builder.icon(tauri::include_image!("icons/128x128@2x.png"))?;
    }
    let window = isolate(builder, &profiles, profile).build()?;
    crate::shell::reapply_badge(app);

    // A window that never finishes loading must still appear.
    let fallback = window.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(Duration::from_secs(5)).await;
        reveal(&fallback, &shown);
    });

    // Seeded before it can be maximized, from what it was built with.
    let normal = NormalBounds::seeded(seed(
        (placement.x, placement.y),
        (placement.width, placement.height),
        bounds_of(&window.as_ref().window()),
    ));
    if restored.maximized {
        let _ = window.maximize();
    }
    // Under "system" the real preference is known now; the window is still hidden.
    repaint(app, theme.as_deref());
    wire_state(app, &window, normal);
    watch_crashes(app, &window);
    #[cfg(windows)]
    switch_browser_accelerators_off(&window);
    #[cfg(windows)]
    refuse_notifications(&window);
    Ok(())
}

/// What the main webview's every load start and finish does, beside showing
/// the window.
pub fn main_page_load<R: Runtime>(app: &AppHandle<R>, event: &tauri::webview::PageLoadEvent) {
    // A reload or a navigation leaves the old page's event streams running
    // (WP2), and its side panel guest over the new page.
    if let tauri::webview::PageLoadEvent::Started = event {
        if let Some(streams) = app.try_state::<crate::event_source::Streams>() {
            streams.close_all();
        }
        crate::side_panel::close_all(app);
    }
    crate::shell::on_page_load(app, event);
}

fn reveal<R: Runtime>(window: &WebviewWindow<R>, shown: &AtomicBool) {
    if !shown.swap(true, Ordering::SeqCst) {
        let _ = window.show();
        let _ = window.set_focus();
    }
}

/// The 500 ms debounce, and the save on close (`createWindow`'s `persist`).
fn wire_state<R: Runtime>(app: &AppHandle<R>, window: &WebviewWindow<R>, normal: NormalBounds) {
    let normal = Arc::new(normal);
    *app.state::<Chrome>().normal.lock().unwrap() = Arc::clone(&normal);
    let persist = {
        let (app, window, normal) = (app.clone(), window.as_ref().window(), Arc::clone(&normal));
        debounce(
            move |()| save_state(&app, &window, &normal),
            500,
            Arc::new(StdTimers::default()),
        )
    };
    let (app, saved) = (app.clone(), window.as_ref().window());
    window.on_window_event(move |event| match event {
        WindowEvent::Resized(_) | WindowEvent::Moved(_) => persist.call(()),
        WindowEvent::CloseRequested { .. } => save_state(&app, &saved, &normal),
        _ => {}
    });
}

/// `network-service.js` is obsolete (the network is in-process); what can die
/// here is the page's own process, and the window is reloaded.
#[cfg(target_os = "linux")]
fn watch_crashes<R: Runtime>(app: &AppHandle<R>, window: &WebviewWindow<R>) {
    use agentrq_desktop_core::webview_crash::Crash;
    use std::time::Instant;
    use webkit2gtk::{WebProcessTerminationReason as Why, WebViewExt};
    let app = app.clone();
    let _ = window.with_webview(move |webview| {
        webview
            .inner()
            .connect_web_process_terminated(move |view, why| {
                let crash = match why {
                    Why::TerminatedByApi => Crash::Requested,
                    Why::ExceededMemoryLimit => Crash::OutOfMemory,
                    _ => Crash::Failed,
                };
                if app.state::<Chrome>().reloads.allow(crash, Instant::now()) {
                    view.reload();
                }
            });
    });
}

/// WebView2's own Ctrl+R, Ctrl+Shift+R and Ctrl+Shift+I run as the key's default
/// action, beside the page's dispatch of the same menu keys (`core::menu_keys`).
#[cfg(windows)]
fn switch_browser_accelerators_off<R: Runtime>(window: &WebviewWindow<R>) {
    use webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2Settings3;
    use windows::core::Interface;
    let _ = window.with_webview(|webview| unsafe {
        let Ok(core) = webview.controller().CoreWebView2() else {
            return;
        };
        if let Ok(settings) = core
            .Settings()
            .and_then(|s| s.cast::<ICoreWebView2Settings3>())
        {
            let _ = settings.SetAreBrowserAcceleratorKeysEnabled(false);
        }
    });
}

/// The app's own page never notifies through its webview, so WebView2 is
/// never left to ask the person itself (the Workspace settings' Enable Push):
/// `core::main_refuses_webview2`'s kinds are denied at once, the rest left
/// to WebView2.
#[cfg(windows)]
fn refuse_notifications<R: Runtime>(window: &WebviewWindow<R>) {
    use agentrq_desktop_core::side_panel::platform_permissions::main_refuses_webview2;
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        COREWEBVIEW2_PERMISSION_KIND, COREWEBVIEW2_PERMISSION_STATE_DENY,
    };
    use webview2_com::PermissionRequestedEventHandler;
    let _ = window.with_webview(|webview| unsafe {
        let Ok(core) = webview.controller().CoreWebView2() else {
            return;
        };
        let handler = PermissionRequestedEventHandler::create(Box::new(|_, args| {
            let Some(args) = args else { return Ok(()) };
            let mut kind = COREWEBVIEW2_PERMISSION_KIND::default();
            args.PermissionKind(&mut kind)?;
            // Throwaway: the handler stubbed out, to see the CI step fail.
            if main_refuses_webview2(kind.0) && std::env::var_os("AGENTRQ_NEVER_SET").is_some() {
                args.SetState(COREWEBVIEW2_PERMISSION_STATE_DENY)?;
            }
            Ok(())
        }));
        let mut token = Default::default();
        let _ = core.add_PermissionRequested(&handler, &mut token);
    });
}

#[cfg(windows)]
fn watch_crashes<R: Runtime>(app: &AppHandle<R>, window: &WebviewWindow<R>) {
    use agentrq_desktop_core::webview_crash::Crash;
    use std::time::Instant;
    use webview2_com::Microsoft::Web::WebView2::Win32::COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED;
    use webview2_com::ProcessFailedEventHandler;
    let app = app.clone();
    let _ = window.with_webview(move |webview| unsafe {
        let Ok(core) = webview.controller().CoreWebView2() else {
            return;
        };
        let reload = core.clone();
        let handler = ProcessFailedEventHandler::create(Box::new(move |_, args| {
            let mut kind = Default::default();
            if let Some(args) = args {
                let _ = args.ProcessFailedKind(&mut kind);
            }
            if kind == COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED
                && app
                    .state::<Chrome>()
                    .reloads
                    .allow(Crash::Failed, Instant::now())
            {
                let _ = reload.Reload();
            }
            Ok(())
        }));
        let mut token = Default::default();
        let _ = core.add_ProcessFailed(&handler, &mut token);
    });
}

/// WKWebView reports the termination to its navigation delegate, which wry
/// owns and does not expose, so there is nothing to listen to here.
#[cfg(target_os = "macos")]
fn watch_crashes<R: Runtime>(_app: &AppHandle<R>, _window: &WebviewWindow<R>) {}

/// `app.on('activate')`, `before-quit`, `window-all-closed` and nothing else.
pub fn on_run_event<R: Runtime>(app: &AppHandle<R>, event: &RunEvent) {
    match event {
        // Closing the last window keeps the app running on macOS, as
        // `window-all-closed` does, so a Dock click can open another. Tauri
        // exits on every platform unless this says otherwise.
        #[cfg(target_os = "macos")]
        RunEvent::ExitRequested {
            code: None, api, ..
        } => {
            api.prevent_exit();
            save_main(app);
        }
        // The Dock icon was clicked with no window open.
        #[cfg(target_os = "macos")]
        RunEvent::Reopen {
            has_visible_windows: false,
            ..
        } => {
            if main_webview(app).is_none() {
                let _ = open_active_main_window(app);
            }
        }
        // A quit that did not close the window first would lose what the
        // debounced save had not yet written. Cmd+Q and Windows' Exit raise
        // only `Exit`, never `ExitRequested`.
        RunEvent::ExitRequested { .. } | RunEvent::Exit => save_main(app),
        _ => {}
    }
}

/// The main window's place, now. `destroy()` raises no `CloseRequested`, so
/// whatever destroys it saves first.
pub fn save_main<R: Runtime>(app: &AppHandle<R>) {
    if let Some(webview) = main_webview(app) {
        let normal = Arc::clone(&app.state::<Chrome>().normal.lock().unwrap());
        save_state(app, &webview.window(), &normal);
    }
}

// ---- the window's view: zoom and full screen ----

pub struct View<'a, R: Runtime> {
    webview: &'a Webview<R>,
    window: Window<R>,
    zoom: &'a ZoomTable,
}

/// The view of a window, for the menu as well as the title bar's commands.
pub fn view_of<'a, R: Runtime>(webview: &'a Webview<R>, chrome: &'a Chrome) -> View<'a, R> {
    View {
        webview,
        window: webview.window(),
        zoom: &chrome.zoom,
    }
}

/// The main window's zoom factor, which the side panel's guest is placed by.
pub fn zoom_of<R: Runtime>(app: &AppHandle<R>) -> f64 {
    app.state::<Chrome>().zoom.get(MAIN)
}

impl<R: Runtime> MaximizeWindow for View<'_, R> {
    fn is_maximized(&self) -> bool {
        self.window.is_maximized().unwrap_or(false)
    }
    fn maximize(&self) {
        let _ = self.window.maximize();
    }
    fn unmaximize(&self) {
        let _ = self.window.unmaximize();
    }
}

impl<R: Runtime> ViewWindow for View<'_, R> {
    fn zoom_factor(&self) -> f64 {
        self.zoom.get(self.webview.label())
    }
    fn set_zoom_factor(&self, factor: f64) {
        if self.webview.set_zoom(factor).is_ok() {
            self.zoom.set(self.webview.label(), factor);
        }
    }
    fn is_full_screen(&self) -> bool {
        self.window.is_fullscreen().unwrap_or(false)
    }
    fn set_full_screen(&self, full_screen: bool) {
        let _ = self.window.set_fullscreen(full_screen);
    }
}

/// `agentrq:view:get`
#[tauri::command]
pub fn view_get<R: Runtime>(webview: Webview<R>) -> Value {
    let chrome = webview.state::<Chrome>();
    view_state(&view_of(&webview, &chrome)).to_json()
}

/// `agentrq:view:zoom`
#[tauri::command]
pub fn view_zoom<R: Runtime>(webview: Webview<R>, direction: String) -> Value {
    let chrome = webview.state::<Chrome>();
    zoom_window(
        &view_of(&webview, &chrome),
        ZoomDirection::parse(&direction),
    )
    .to_json()
}

/// `agentrq:view:fullscreen`
#[tauri::command]
pub fn view_fullscreen<R: Runtime>(webview: Webview<R>) -> Value {
    let chrome = webview.state::<Chrome>();
    toggle_full_screen(&view_of(&webview, &chrome)).to_json()
}

// ---- theme ----

struct Native<R: Runtime>(AppHandle<R>);

impl<R: Runtime> NativeTheme for Native<R> {
    fn set_theme_source(&self, source: ThemeSource) {
        self.0.set_theme(match source {
            ThemeSource::System => None,
            ThemeSource::Light => Some(Theme::Light),
            ThemeSource::Dark => Some(Theme::Dark),
        });
    }
    fn should_use_dark_colors(&self) -> bool {
        main_webview(&self.0).and_then(|webview| webview.window().theme().ok()) == Some(Theme::Dark)
    }
}

struct Paint<R: Runtime>(Webview<R>);

impl<R: Runtime> ThemedWindow for Paint<R> {
    fn set_background_color(&self, color: &str) {
        let _ = self.0.window().set_background_color(color_of(color));
        let _ = self.0.set_background_color(color_of(color));
    }
}

fn repaint<R: Runtime>(app: &AppHandle<R>, theme: Option<&str>) -> Value {
    apply_theme(theme, &Native(app.clone()), || {
        // Not the side panel's guests: a page there paints its own, on white.
        app.webviews()
            .into_values()
            .filter(|webview| !is_guest_label(webview.label()))
            .map(|webview| Arc::new(Paint(webview)) as Arc<dyn ThemedWindow>)
            .collect()
    })
    .to_json()
}

/// `agentrq:theme:set`
#[tauri::command]
pub fn theme_set<R: Runtime>(app: AppHandle<R>, theme: String) -> Value {
    *app.state::<Chrome>().theme.lock().unwrap() = Some(theme.clone());
    repaint(&app, Some(&theme))
}

// ---- the small native calls ----

/// `agentrq:dialog:chooseDirectory`: `''` when dismissed. `current_path` is
/// optional because the form may have nothing in the field yet.
#[tauri::command]
pub async fn dialog_choose_directory<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    current_path: Option<String>,
) -> String {
    let mut dialog = app
        .dialog()
        .file()
        .set_title("Choose a working directory")
        .set_can_create_directories(true)
        .set_parent(&webview.window());
    if let Some(start) = chooser_start(current_path.as_deref()) {
        dialog = dialog.set_directory(start);
    }
    dialog
        .blocking_pick_folder()
        .and_then(|path| path.into_path().ok())
        .map(|path| path.to_string_lossy().into_owned())
        .unwrap_or_default()
}

/// `agentrq:clipboard:write`
#[tauri::command]
pub fn clipboard_write<R: Runtime>(app: AppHandle<R>, text: String) -> Result<bool, String> {
    app.clipboard()
        .write_text(text)
        .map(|()| true)
        .map_err(|e| e.to_string())
}

struct Shell;

impl FileShell for Shell {
    fn stat(&self, path: &str) -> Option<bool> {
        std::fs::metadata(path).ok().map(|m| m.is_dir())
    }
    fn reveal(&self, path: &str) {
        let _ = tauri_plugin_opener::reveal_item_in_dir(path);
    }
    fn open(&self, path: &str) -> Result<(), String> {
        tauri_plugin_opener::open_path(path, None::<&str>).map_err(|e| e.to_string())
    }
}

/// `agentrq:files:open`. Only the URL the renderer was given arrives, never a
/// path it composed, and `core` decides open or reveal; no `file:` URL is ever
/// made followable.
#[tauri::command]
pub async fn files_open(file_url: String) -> Value {
    open_file_link(&Shell, &file_url).to_json()
}

struct Emoji<R: Runtime>(AppHandle<R>);

impl<R: Runtime> EmojiPanelHost for Emoji<R> {
    fn open_character_palette(&mut self) {
        #[cfg(target_os = "macos")]
        let _ = self.0.run_on_main_thread(|| {
            use objc2_app_kit::NSApplication;
            use objc2_foundation::MainThreadMarker;
            if let Some(mtm) = MainThreadMarker::new() {
                NSApplication::sharedApplication(mtm).orderFrontCharacterPalette(None);
            }
        });
    }

    fn synthesize_win_period(&mut self) {
        #[cfg(windows)]
        win_period();
    }
}

/// Win+. : the two keys down, then up in the opposite order.
#[cfg(windows)]
fn win_period() {
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::{
        SendInput, INPUT, INPUT_0, INPUT_KEYBOARD, KEYBDINPUT, KEYEVENTF_KEYUP, VK_LWIN,
        VK_OEM_PERIOD,
    };
    let key = |vk: u16, flags: u32| INPUT {
        r#type: INPUT_KEYBOARD,
        Anonymous: INPUT_0 {
            ki: KEYBDINPUT {
                wVk: vk,
                wScan: 0,
                dwFlags: flags,
                time: 0,
                dwExtraInfo: 0,
            },
        },
    };
    let inputs = [
        key(VK_LWIN, 0),
        key(VK_OEM_PERIOD, 0),
        key(VK_OEM_PERIOD, KEYEVENTF_KEYUP),
        key(VK_LWIN, KEYEVENTF_KEYUP),
    ];
    unsafe {
        SendInput(
            inputs.len() as u32,
            inputs.as_ptr(),
            std::mem::size_of::<INPUT>() as i32,
        );
    }
}

/// `agentrq:emoji:show-panel`
#[tauri::command]
pub fn emoji_show_panel<R: Runtime>(app: AppHandle<R>) -> bool {
    show_emoji_panel(PLATFORM, &mut Emoji(app))
}

/// What the loader above uses, so `lib.rs` need not name the plugins.
pub fn plugins<R: Runtime>(builder: tauri::Builder<R>) -> tauri::Builder<R> {
    #[cfg(not(test))]
    let builder = builder
        .plugin(tauri_plugin_single_instance::init(|app, argv, _cwd| {
            crate::shell::handle_second_instance(app, &argv);
        }))
        .plugin(tauri_plugin_global_shortcut::Builder::new().build());

    builder
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_deep_link::init())
        .plugin(tauri_plugin_opener::init())
        .on_window_event(|window, event| {
            crate::shell::handle_window_event(window, event);
        })
}

/// Wire desktop shell hooks for shortcuts and deep links. A macOS link
/// arrives as a run event (`shell::on_run_event`), never through a listener.
pub fn install_hooks<R: Runtime>(app: &AppHandle<R>) {
    use tauri_plugin_deep_link::DeepLinkExt;
    #[cfg(not(test))]
    {
        use tauri_plugin_global_shortcut::GlobalShortcutExt;

        let app_shortcut = app.clone();
        let _ = app.global_shortcut().on_shortcut(
            "CmdOrCtrl+Shift+N",
            move |_app, _shortcut, event| {
                crate::shell::handle_shortcut_event(&app_shortcut, event.state);
            },
        );
    }

    #[cfg(target_os = "linux")]
    let should_register = tauri::is_dev() || std::env::var_os("APPIMAGE").is_some();
    #[cfg(not(target_os = "linux"))]
    let should_register = tauri::is_dev();
    if should_register {
        let _ = app
            .deep_link()
            .register(agentrq_desktop_core::deep_link::DEEP_LINK_SCHEME_TAURI);
    }
}
