// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! WP6a shell features: link routing, deep links, single instance, global shortcut,
//! badge count, and tray menu.
//!
//! Pure logic lives in `agentrq_desktop_core` (`links`, `deep_link`, `tray`, `notifications`, `auth`).
//! This module connects that pure core to Tauri's plugins and window lifecycle.

use std::sync::Arc;
use std::sync::Mutex;

use agentrq_desktop_core::auth::sign_in_start;
use agentrq_desktop_core::deep_link::{
    deep_link_from_argv, parse_deep_link, PendingRoute, DEEP_LINK_SCHEME_TAURI,
};
use agentrq_desktop_core::links::{
    classify_link, is_allowed_download, LinkOptions, LinkTarget, PLATFORM,
};
use agentrq_desktop_core::notifications::badge_for;
use agentrq_desktop_core::tray::{
    action_for_id, build_tray_menu_template, dispatch, new_task_route, RecentWorkspace,
    RecentWorkspaces, TrayActions,
};
#[cfg(target_os = "windows")]
use tauri::image::Image;
use tauri::webview::PageLoadEvent;
use tauri::{AppHandle, Emitter, Manager, Runtime, Url, WindowEvent};
use tauri_plugin_deep_link::DeepLinkExt;
use tauri_plugin_global_shortcut::ShortcutState;
use tauri_plugin_opener::OpenerExt;

use crate::notify::Notifier;

/// State for the desktop shell (unread badges, recent workspaces, deep link pending route).
pub struct ShellState<R: Runtime = tauri::Wry> {
    pub(crate) pending_route: Mutex<PendingRoute>,
    /// The Notifier's count as shown, and its generation.
    unread: Mutex<(u64, i64)>,
    pub(crate) recent_workspaces: Mutex<RecentWorkspaces>,
    _runtime: std::marker::PhantomData<fn() -> R>,
}

impl<R: Runtime> Default for ShellState<R> {
    fn default() -> Self {
        Self {
            pending_route: Mutex::new(PendingRoute::new()),
            unread: Mutex::new((0, 0)),
            recent_workspaces: Mutex::new(RecentWorkspaces::default()),
            _runtime: std::marker::PhantomData,
        }
    }
}

impl<R: Runtime> ShellState<R> {
    /// Navigate the app to an in-app route. If the webview is still loading, the
    /// route is held until page load finishes.
    pub fn navigate(&self, app: &AppHandle<R>, route: &str) {
        if route.is_empty() {
            return;
        }
        focus_main_window(app);
        let to_emit = {
            let mut guard = self.pending_route.lock().unwrap();
            guard.navigate(route)
        };
        if let Some(r) = to_emit {
            let _ = app.emit("agentrq:navigate", r);
        }
    }

    /// Open a deep link URL (e.g. `agentrq-tauri://workspaces/<id>`).
    pub fn open_deep_link(&self, app: &AppHandle<R>, url: &str) {
        focus_main_window(app);
        if let Some(route) = parse_deep_link(url, DEEP_LINK_SCHEME_TAURI) {
            self.navigate(app, &route);
        }
    }

    /// Handle a second instance launching with arguments.
    pub fn on_second_instance(&self, app: &AppHandle<R>, argv: &[String]) {
        focus_main_window(app);
        if let Some(url) = deep_link_from_argv(argv, DEEP_LINK_SCHEME_TAURI) {
            self.open_deep_link(app, &url);
        }
    }

    /// Trigger quick create (e.g. from the global shortcut or tray menu).
    pub fn on_quick_create(&self, app: &AppHandle<R>) {
        let route = {
            let lock = self.recent_workspaces.lock().unwrap();
            new_task_route(lock.list())
        };
        self.navigate(app, &route);
    }

    /// Track page load lifecycle so pending routes are delivered once the renderer is ready.
    pub fn on_page_load(&self, app: &AppHandle<R>, event: &PageLoadEvent) {
        match event {
            PageLoadEvent::Started => self.page_leaving(),
            PageLoadEvent::Finished => {
                let to_emit = {
                    let mut guard = self.pending_route.lock().unwrap();
                    guard.on_page_load_finished()
                };
                if let Some(route) = to_emit {
                    let _ = app.emit("agentrq:navigate", route);
                }
            }
        }
    }

    /// The page is about to go: a route is held from now on, not sent to a
    /// page that will never read it.
    pub fn page_leaving(&self) {
        self.pending_route.lock().unwrap().on_page_load_started();
    }

    /// When renderer subscribes to `agentrq:navigate`, re-emit pending route if waiting.
    pub fn on_subscribed(&self, app: &AppHandle<R>) {
        let to_emit = {
            let mut guard = self.pending_route.lock().unwrap();
            guard.on_subscribed()
        };
        if let Some(route) = to_emit {
            let _ = app.emit("agentrq:navigate", route);
        }
    }

    /// Show the Notifier's unread count in the tray and badge. Counts arrive
    /// after the Notifier's lock is dropped, so in any order: one older than
    /// the count shown is dropped, and the tray and badge show the kept count,
    /// so the last of them to run shows the newest.
    pub fn set_unread_count(&self, app: &AppHandle<R>, count: i64, generation: u64) {
        {
            let mut shown = self.unread.lock().unwrap();
            if generation < shown.0 {
                return;
            }
            *shown = (generation, count);
        }
        self.refresh_tray(app);
        self.set_badge_count(app, self.unread_count());
    }

    /// The unread count shown.
    pub fn unread_count(&self) -> i64 {
        self.unread.lock().unwrap().1
    }

    /// Set the application badge count (macOS/Linux: dock badge, Windows: taskbar overlay).
    pub fn set_badge_count(&self, app: &AppHandle<R>, count: i64) {
        let state = badge_for(count, PLATFORM);
        if let Some(win) = app.get_window("main") {
            #[cfg(target_os = "windows")]
            {
                if let Some(overlay) = state.overlay {
                    let rgba = render_badge_overlay(overlay.count);
                    let img = Image::new_owned(rgba, 16, 16);
                    let _ = win.set_overlay_icon(Some(img));
                } else {
                    let _ = win.set_overlay_icon(None);
                }
            }
            #[cfg(not(target_os = "windows"))]
            {
                let badge_num = if state.badge.is_empty() {
                    None
                } else {
                    state.badge.parse::<i64>().ok()
                };
                let _ = win.set_badge_count(badge_num);
            }
        }
    }

    /// Update recent workspaces.
    #[allow(dead_code)]
    pub fn set_recent_workspaces(&self, app: &AppHandle<R>, workspaces: Vec<RecentWorkspace>) {
        let mut rw = RecentWorkspaces::default();
        for w in workspaces {
            rw.touch(&w.id, Some(&w.name));
        }
        *self.recent_workspaces.lock().unwrap() = rw;
        self.refresh_tray(app);
    }

    /// Touch a recent workspace.
    pub fn touch_recent_workspace(&self, app: &AppHandle<R>, id: &str, name: Option<&str>) {
        self.recent_workspaces.lock().unwrap().touch(id, name);
        self.refresh_tray(app);
    }

    /// Another profile's workspaces are no use in this one's tray, nor for its
    /// New Task shortcut.
    pub fn forget_recent_workspaces(&self, app: &AppHandle<R>) {
        *self.recent_workspaces.lock().unwrap() = RecentWorkspaces::default();
        self.refresh_tray(app);
    }

    /// Re-render the tray menu and tooltip with the latest unread count and recent workspaces.
    pub fn refresh_tray(&self, app: &AppHandle<R>) {
        let unread = self.unread_count();
        let workspaces = self.recent_workspaces.lock().unwrap().list().to_vec();
        crate::app_menu::update_tray(app, &workspaces, unread);
    }
}

/// Bring the main window to the front and unminimize it if minimized.
pub fn focus_main_window<R: Runtime>(app: &AppHandle<R>) -> Option<tauri::Window<R>> {
    let win = app.get_window("main")?;
    let _ = win.unminimize();
    let _ = win.show();
    let _ = win.set_focus();
    Some(win)
}

/// Re-apply badge count when a main window is reopened or rebuilt.
pub fn reapply_badge<R: Runtime>(app: &AppHandle<R>) {
    if let Some(state) = app.try_state::<ShellState<R>>() {
        let count = state.unread_count();
        state.set_badge_count(app, count);
    }
}

/// Renders a 16x16 RGBA badge overlay icon (a filled red dot) for the taskbar.
#[allow(dead_code)]
pub fn render_badge_overlay(_count: i64) -> Vec<u8> {
    let mut pixels = vec![0u8; 16 * 16 * 4];
    for y in 0..16i32 {
        for x in 0..16i32 {
            let dx = x - 8;
            let dy = y - 8;
            if dx * dx + dy * dy <= 36 {
                let idx = ((y * 16 + x) * 4) as usize;
                pixels[idx] = 239; // R (#ef4444)
                pixels[idx + 1] = 68; // G
                pixels[idx + 2] = 68; // B
                pixels[idx + 3] = 255; // A
            }
        }
    }
    pixels
}

/// Expose page load handler to chrome window lifecycle.
pub fn on_page_load<R: Runtime>(app: &AppHandle<R>, event: &PageLoadEvent) {
    if let Some(state) = app.try_state::<ShellState<R>>() {
        state.on_page_load(app, event);
    }
}

/// Dispatches link navigation in the main webview.
///
/// Sends off-origin URLs to the system browser (via opener) and blocks dangerous schemes.
/// Intercepts sign-in URLs first to open the dedicated WP4 sign-in window.
/// Keeps app origin and safe downloads in the app.
pub fn dispatch_navigation<R: Runtime>(app: &AppHandle<R>, url: &Url) -> bool {
    let (active_id, server_url) =
        if let Some(profiles) = app.try_state::<Arc<crate::profiles::Profiles>>() {
            (profiles.active_id(), profiles.server_url())
        } else {
            ("default".to_string(), String::new())
        };
    let app_origin = crate::profiles::app_origin(&active_id);

    if let Some(start) = sign_in_start(url.as_str(), &app_origin, &server_url) {
        let app = app.clone();
        tauri::async_runtime::spawn(async move { crate::open_sign_in(&app, start) });
        return false;
    }

    // WebKitGTK evaluates on_navigation for all frames including subframes; we allow
    // safe downloads and app-origin URLs to stay in-webview before handing off-origin
    // URLs to the system opener.
    if is_allowed_download(url, &app_origin) {
        return true;
    }

    match classify_link(
        url.as_str(),
        LinkOptions {
            app_origin: &app_origin,
            server_url: &server_url,
        },
    ) {
        LinkTarget::App | LinkTarget::SignIn => true,
        LinkTarget::System => {
            let _ = app.opener().open_url(url.as_str(), None::<&str>);
            false
        }
        LinkTarget::Blocked => false,
    }
}

/// Dispatches new window requests (`window.open`).
/// Routes off-origin links to the system opener and denies opening a second webview window.
pub fn dispatch_new_window<R: Runtime>(
    app: &AppHandle<R>,
    url: &Url,
) -> tauri::webview::NewWindowResponse<R> {
    dispatch_navigation(app, url);
    tauri::webview::NewWindowResponse::Deny
}

/// Handle a click on a tray menu item by dispatching to TrayActions via core's template parser.
pub(crate) fn handle_tray_menu_event<R: Runtime>(app: &AppHandle<R>, id: &str) {
    if let Some(state) = app.try_state::<ShellState<R>>() {
        let template = {
            let unread = state.unread_count();
            let workspaces = state.recent_workspaces.lock().unwrap().list().to_vec();
            build_tray_menu_template(&workspaces, unread)
        };
        if let Some(action) = action_for_id(&template, id) {
            struct ShellTrayActions<'a, R: Runtime> {
                app: &'a AppHandle<R>,
                state: &'a ShellState<R>,
            }
            impl<'a, R: Runtime> TrayActions for ShellTrayActions<'a, R> {
                fn open(&mut self) {
                    focus_main_window(self.app);
                }
                fn new_task(&mut self) {
                    self.state.on_quick_create(self.app);
                }
                fn open_workspace(&mut self, id: &str) {
                    self.state.navigate(self.app, &format!("/workspaces/{id}"));
                }
                fn quit(&mut self) {
                    crate::app_menu::quit(self.app);
                }
            }
            let mut actions = ShellTrayActions {
                app,
                state: state.inner(),
            };
            dispatch(&action, &mut actions);
        }
    }
}

/// Handle second instance argv dispatch.
pub(crate) fn handle_second_instance<R: Runtime>(app: &AppHandle<R>, argv: &[String]) {
    focus_main_window(app);
    if let Some(state) = app.try_state::<ShellState<R>>() {
        state.on_second_instance(app, argv);
    }
}

/// Looking at the app answers "is there anything new". The count is the
/// Notifier's, which publishes the cleared badge and tray.
pub(crate) fn handle_window_event<R: Runtime>(window: &tauri::Window<R>, event: &WindowEvent) {
    if window.label() == "main" {
        if let WindowEvent::Focused(true) = event {
            if let Some(notifier) = window.app_handle().try_state::<Arc<Notifier>>() {
                notifier.clear_unread();
            }
        }
    }
}

/// Handle global shortcut key press.
pub(crate) fn handle_shortcut_event<R: Runtime>(app: &AppHandle<R>, state: ShortcutState) {
    if state == ShortcutState::Pressed {
        if let Some(shell_state) = app.try_state::<ShellState<R>>() {
            shell_state.on_quick_create(app);
        }
    }
}

/// A link macOS asks the app to open. At a cold start AppKit may send it
/// before setup: then there is no state yet and this drops it, and `init`
/// routes it from the plugin's `get_current`. Never through the plugin's
/// `on_open_url`, a Rust listener: Tauri queues an emit that meets another
/// thread's.
#[cfg(target_os = "macos")]
pub(crate) fn on_run_event<R: Runtime>(app: &AppHandle<R>, event: &tauri::RunEvent) {
    let state = app.try_state::<ShellState<R>>();
    if let (tauri::RunEvent::Opened { urls }, Some(state)) = (event, state) {
        for url in urls {
            state.open_deep_link(app, url.as_str());
        }
    }
}

/// Inspect first launch arguments or current URL for initial deep link route.
pub(crate) fn check_first_launch_route<R: Runtime>(
    app: &AppHandle<R>,
    args: &[String],
    current_urls: Option<Vec<Url>>,
) {
    if let Some(state) = app.try_state::<ShellState<R>>() {
        if let Some(url) = deep_link_from_argv(args, DEEP_LINK_SCHEME_TAURI) {
            if let Some(route) = parse_deep_link(&url, DEEP_LINK_SCHEME_TAURI) {
                state.pending_route.lock().unwrap().set_pending(&route);
            }
        } else if let Some(urls) = current_urls {
            if let Some(url) = urls.first() {
                if let Some(route) = parse_deep_link(url.as_str(), DEEP_LINK_SCHEME_TAURI) {
                    state.pending_route.lock().unwrap().set_pending(&route);
                }
            }
        }
    }
}

/// The main window is about to be replaced or sent elsewhere. Its next
/// `Started` comes too late: a click in between would go to the old page.
pub fn page_leaving<R: Runtime>(app: &AppHandle<R>) {
    if let Some(state) = app.try_state::<ShellState<R>>() {
        state.page_leaving();
    }
}

/// Signals that the renderer's navigation listener is subscribed and ready.
pub fn navigation_ready<R: Runtime>(app: &AppHandle<R>) {
    if let Some(state) = app.try_state::<ShellState<R>>() {
        state.on_subscribed(app);
    }
}

/// Initialize shell services: state management and startup deep link. The
/// plugin's `get_current` is the only route for a macOS link sent before
/// setup (see `on_run_event`).
pub fn init<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    app.manage(ShellState::<R>::default());

    // First-launch argv deep link
    let args: Vec<String> = std::env::args().collect();
    let current_urls = app.deep_link().get_current().ok().flatten();
    check_first_launch_route(app, &args, current_urls);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::events::Events;
    use crate::notify::tests::{reply_event, FakeDesk};
    use crate::side_panel::tests::Heard;
    use std::sync::Arc;
    use tauri::Listener;

    #[cfg(windows)]
    const LOCAL: &str = "http://tauri.localhost";
    #[cfg(not(windows))]
    const LOCAL: &str = "tauri://localhost";

    fn test_app() -> (
        tauri::App<tauri::test::MockRuntime>,
        tauri::AppHandle<tauri::test::MockRuntime>,
    ) {
        let mut ctx = tauri::test::mock_context(tauri::test::noop_assets());
        ctx.config_mut().identifier = "com.agentrq.desktop.tauri".to_string();
        ctx.config_mut().plugins.0.insert(
            "deep-link".into(),
            serde_json::json!({"desktop": {"schemes": [DEEP_LINK_SCHEME_TAURI]}}),
        );
        let app = crate::chrome::plugins(tauri::test::mock_builder())
            .invoke_handler(crate::commands::handler())
            .build(ctx)
            .unwrap();
        let handle = app.handle().clone();
        (app, handle)
    }

    #[test]
    fn mock_app_navigate_before_load_delivers_on_finished() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();

        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });

        // 0. Empty route navigate is no-op
        state.navigate(&handle, "");
        // 0b. Invalid deep link is no-op
        state.open_deep_link(&handle, "not-deep-link");

        // 1. Navigate before load: route is buffered
        state.open_deep_link(&handle, "agentrq-tauri://workspaces/1");
        assert!(delivered.lock().unwrap().is_empty());

        // 2. Finished delivery: renderer not subscribed yet, so stays buffered
        on_page_load(&handle, &PageLoadEvent::Finished);
        assert!(delivered.lock().unwrap().is_empty());

        // 3. Renderer subscription signal delivers the pending route once
        state.on_subscribed(&handle);
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/1\""]);

        // 4. Repeated Finished or subscription call does not deliver again
        on_page_load(&handle, &PageLoadEvent::Finished);
        state.on_subscribed(&handle);
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/1\""]);

        // 5. Once subscribed and page loaded, subsequent navigate delivers immediately
        delivered.lock().unwrap().clear();
        state.navigate(&handle, "/workspaces/2");
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/2\""]);

        // 6. Started resets
        delivered.lock().unwrap().clear();
        on_page_load(&handle, &PageLoadEvent::Started);
        state.navigate(&handle, "/workspaces/3");
        assert!(delivered.lock().unwrap().is_empty());
    }

    #[test]
    fn mock_app_second_instance_delivers_deep_link() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();

        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });

        on_page_load(&handle, &PageLoadEvent::Finished);
        state.on_subscribed(&handle);

        let argv = vec![
            "agentrq-tauri".to_string(),
            "agentrq-tauri://workspaces/ws1/tasks/t1".to_string(),
        ];
        handle_second_instance(&handle, &argv);

        assert_eq!(
            *delivered.lock().unwrap(),
            vec!["\"/workspaces/ws1/tasks/t1\""]
        );

        // An argv without deep links does not navigate
        delivered.lock().unwrap().clear();
        handle_second_instance(&handle, &["agentrq-tauri".to_string()]);
        assert!(delivered.lock().unwrap().is_empty());
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn a_link_macos_opens_arrives_as_a_run_event() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();

        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });
        let opened = |url: &str| tauri::RunEvent::Opened {
            urls: vec![Url::parse(url).unwrap()],
        };

        // At a cold start the link comes before the page: it is held.
        on_run_event(&handle, &opened("agentrq-tauri://workspaces/cold"));
        on_run_event(&handle, &tauri::RunEvent::Ready);
        assert!(delivered.lock().unwrap().is_empty());
        on_page_load(&handle, &PageLoadEvent::Finished);
        state.on_subscribed(&handle);
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/cold\""]);

        // While the app runs, it goes to the page at once.
        delivered.lock().unwrap().clear();
        on_run_event(&handle, &opened("agentrq-tauri://workspaces/warm"));
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/warm\""]);
    }

    /// A link the plugin holds when setup runs is the cold start's, and
    /// `init` routes it. Only macOS fills it from a run event, which a mock
    /// cannot send, so here the plugin reads it from argv.
    #[cfg(not(target_os = "macos"))]
    #[test]
    fn init_routes_the_link_the_plugin_holds() {
        let (_app, handle) = test_app();
        let navigate = Heard::new(&handle, "agentrq:navigate");
        handle
            .deep_link()
            .handle_cli_arguments(["app", "agentrq-tauri://workspaces/early"].into_iter());
        init(&handle).unwrap();
        on_page_load(&handle, &PageLoadEvent::Finished);
        navigation_ready(&handle);
        assert_eq!(navigate.all(), [serde_json::json!("/workspaces/early")]);
    }

    /// AppKit may send a cold start's link before setup, which then is left
    /// to `init` (see `init_routes_the_link_the_plugin_holds`).
    #[cfg(target_os = "macos")]
    #[test]
    fn a_link_before_setup_is_left_to_init() {
        let (_app, handle) = test_app();
        let navigate = Heard::new(&handle, "agentrq:navigate");
        on_run_event(
            &handle,
            &tauri::RunEvent::Opened {
                urls: vec![Url::parse("agentrq-tauri://workspaces/early").unwrap()],
            },
        );
        assert!(handle
            .try_state::<ShellState<tauri::test::MockRuntime>>()
            .is_none());
        init(&handle).unwrap();
        on_page_load(&handle, &PageLoadEvent::Finished);
        navigation_ready(&handle);
        assert!(navigate.all().is_empty());
    }

    #[test]
    fn mock_app_tray_menu_dispatch() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();

        state.set_recent_workspaces(
            &handle,
            vec![RecentWorkspace {
                id: "ws1".to_string(),
                name: "Alpha".to_string(),
            }],
        );

        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });

        on_page_load(&handle, &PageLoadEvent::Finished);
        state.on_subscribed(&handle);

        // New task dispatch
        handle_tray_menu_event(&handle, "new-task");
        assert_eq!(
            *delivered.lock().unwrap(),
            vec!["\"/workspaces/ws1/tasks/new\""]
        );

        // Workspace dispatch
        delivered.lock().unwrap().clear();
        handle_tray_menu_event(&handle, "workspace-ws1");
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/ws1\""]);

        // Open dispatch
        handle_tray_menu_event(&handle, "open");

        // Quit dispatch
        handle_tray_menu_event(&handle, "quit");

        // Unknown id is ignored
        handle_tray_menu_event(&handle, "unknown-id");
    }

    #[test]
    fn mock_app_forgets_the_last_profiles_workspaces() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();
        state.touch_recent_workspace(&handle, "ws5", Some("Team 5"));
        state.forget_recent_workspaces(&handle);
        assert!(state.recent_workspaces.lock().unwrap().list().is_empty());
    }

    #[test]
    fn mock_app_shortcut_and_badge() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();

        state.touch_recent_workspace(&handle, "ws5", Some("Team 5"));

        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });

        on_page_load(&handle, &PageLoadEvent::Finished);
        state.on_subscribed(&handle);

        // Shortcut event testing
        handle_shortcut_event(&handle, ShortcutState::Pressed);
        assert_eq!(
            *delivered.lock().unwrap(),
            vec!["\"/workspaces/ws5/tasks/new\""]
        );
        handle_shortcut_event(&handle, ShortcutState::Released);

        // Badge without main window
        state.set_unread_count(&handle, 1, 1);
        reapply_badge(&handle);

        // Window focus, unread count, badge and window event
        let win = tauri::WebviewWindowBuilder::new(&handle, "main", tauri::WebviewUrl::default())
            .build()
            .unwrap();
        let _ = win.minimize();
        assert!(focus_main_window(&handle).is_some());

        state.set_unread_count(&handle, 5, 2);
        assert_eq!(state.unread_count(), 5);
        reapply_badge(&handle);

        // Focus clears the Notifier's count, which the Desk then shows.
        let desk = Arc::new(FakeDesk::default());
        let dir = tempfile::tempdir().unwrap();
        let notifier = Arc::new(Notifier::new(
            Arc::new(crate::network::tests::network(dir.path(), "")),
            desk.clone(),
        ));
        handle.manage(notifier.clone());
        notifier.on_event(&reply_event("t1", "agent", "m1"));
        handle_window_event(&win.as_ref().window(), &WindowEvent::Focused(true));
        handle_window_event(&win.as_ref().window(), &WindowEvent::Focused(false));
        assert_eq!(*desk.unread.lock().unwrap(), [1, 0]);
        state.set_unread_count(&handle, 0, 3);

        // Readiness signal via navigation_ready
        navigation_ready(&handle);

        // First launch argv / current URLs check
        check_first_launch_route(
            &handle,
            &["app".into(), "agentrq-tauri://workspaces/w_argv".into()],
            None,
        );
        check_first_launch_route(
            &handle,
            &["app".into(), "agentrq-tauri://invalid".into()],
            None,
        );
        check_first_launch_route(
            &handle,
            &["app".into()],
            Some(vec![Url::parse("agentrq-tauri://workspaces/w_cur").unwrap()]),
        );
        check_first_launch_route(
            &handle,
            &["app".into()],
            Some(vec![Url::parse("https://example.com").unwrap()]),
        );
        check_first_launch_route(&handle, &["app".into()], Some(vec![]));
        check_first_launch_route(&handle, &["app".into()], None);
    }

    #[test]
    fn unmanaged_state_handlers_are_safe_noops() {
        let (_app, handle) = test_app();
        handle_second_instance(&handle, &["app".into()]);
        handle_shortcut_event(&handle, ShortcutState::Pressed);
        #[cfg(target_os = "macos")]
        on_run_event(
            &handle,
            &tauri::RunEvent::Opened {
                urls: vec![Url::parse("agentrq-tauri://workspaces/1").unwrap()],
            },
        );
        check_first_launch_route(&handle, &["app".into()], None);
        handle_tray_menu_event(&handle, "open");
        on_page_load(&handle, &PageLoadEvent::Started);
        page_leaving(&handle);
        reapply_badge(&handle);
    }

    #[test]
    fn window_event_ignores_non_main_windows() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let other_win =
            tauri::WebviewWindowBuilder::new(&handle, "other", tauri::WebviewUrl::default())
                .build()
                .unwrap();
        handle_window_event(&other_win.as_ref().window(), &WindowEvent::Focused(true));
    }

    #[test]
    fn dispatches_navigation_and_new_window() {
        let (_app, handle) = test_app();
        let origin = crate::profiles::app_origin("default");

        // 1. App origin URL is allowed
        let app_url = Url::parse(&format!("{origin}/tasks")).unwrap();
        assert!(dispatch_navigation(&handle, &app_url));

        // 2. Blob and data download URLs are allowed
        let blob_url = Url::parse(&format!("blob:{origin}/abc-123")).unwrap();
        assert!(dispatch_navigation(&handle, &blob_url));

        let data_url = Url::parse("data:text/plain;base64,aGVsbG8=").unwrap();
        assert!(dispatch_navigation(&handle, &data_url));

        // Top-level HTML, SVG, and XML data navigation is blocked
        let data_html = Url::parse("data:text/html,<script>alert(1)</script>").unwrap();
        assert!(!dispatch_navigation(&handle, &data_html));

        let data_svg = Url::parse("data:image/svg+xml,<svg onload='alert(1)'/>").unwrap();
        assert!(!dispatch_navigation(&handle, &data_svg));

        let data_xml = Url::parse("data:text/xml,<root/>").unwrap();
        assert!(!dispatch_navigation(&handle, &data_xml));

        // Allowed download: PDF
        let data_pdf = Url::parse("data:application/pdf;base64,JVBERi0xLjQK").unwrap();
        assert!(dispatch_navigation(&handle, &data_pdf));

        // 3. External URLs are rejected and opened via opener
        let ext_url = Url::parse("https://agentrq.com/docs").unwrap();
        assert!(!dispatch_navigation(&handle, &ext_url));

        // 4. Dangerous scheme is blocked
        let bad_url = Url::parse("file:///etc/passwd").unwrap();
        assert!(!dispatch_navigation(&handle, &bad_url));

        // 5. dispatch_new_window denies window opening
        let resp = dispatch_new_window(&handle, &app_url);
        assert!(matches!(resp, tauri::webview::NewWindowResponse::Deny));

        // 6. Sign-in URL goes to WP4's flow (dedicated sign-in window)
        let rig = crate::profiles::tests::rig_with(None, Some("https://server.example.com"));
        handle.manage(rig.profiles.clone());
        let signin_url = Url::parse("https://server.example.com/api/v1/auth/google/login").unwrap();
        assert!(!dispatch_navigation(&handle, &signin_url));
        crate::side_panel::tests::wait_for("WP4's window", &|| {
            handle
                .windows()
                .into_keys()
                .any(|l| l.starts_with("sign-in-"))
        });
    }

    #[test]
    fn badge_overlay_pixels_are_not_transparent() {
        let pixels = render_badge_overlay(3);
        assert_eq!(pixels.len(), 16 * 16 * 4);
        assert!(pixels.iter().any(|&b| b != 0));
        let solid_pixels = pixels.chunks(4).filter(|p| p[3] == 255).count();
        assert!(solid_pixels > 0);
    }

    #[test]
    fn navigation_ready_ipc_delivers_pending_route_through_handler() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();
        state.navigate(&handle, "/cold-start");

        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });

        on_page_load(&handle, &PageLoadEvent::Finished);
        assert!(delivered.lock().unwrap().is_empty());

        let win = tauri::WebviewWindowBuilder::new(&handle, "main", tauri::WebviewUrl::default())
            .build()
            .unwrap();
        let resp = tauri::test::get_ipc_response(
            &win,
            tauri::webview::InvokeRequest {
                cmd: "navigation_ready".into(),
                callback: tauri::ipc::CallbackFn(0),
                error: tauri::ipc::CallbackFn(1),
                url: LOCAL.parse().unwrap(),
                body: tauri::ipc::InvokeBody::default(),
                headers: Default::default(),
                invoke_key: tauri::test::INVOKE_KEY.to_string(),
            },
        );
        assert!(resp.is_ok());
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/cold-start\""]);

        // Calling it again does not redeliver
        let resp2 = tauri::test::get_ipc_response(
            &win,
            tauri::webview::InvokeRequest {
                cmd: "navigation_ready".into(),
                callback: tauri::ipc::CallbackFn(0),
                error: tauri::ipc::CallbackFn(1),
                url: LOCAL.parse().unwrap(),
                body: tauri::ipc::InvokeBody::default(),
                headers: Default::default(),
                invoke_key: tauri::test::INVOKE_KEY.to_string(),
            },
        );
        assert!(resp2.is_ok());
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/cold-start\""]);
    }

    #[test]
    fn delivers_pending_route_on_finished_if_already_subscribed() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();

        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });

        // Started resets ready state
        on_page_load(&handle, &PageLoadEvent::Started);

        // Renderer subscribes while page is still loading
        state.on_subscribed(&handle);

        // Route arrives while page is still loading
        state.navigate(&handle, "/workspaces/subscribed-first");
        assert!(delivered.lock().unwrap().is_empty());

        // Finished fires and delivers the pending route because it was already subscribed
        on_page_load(&handle, &PageLoadEvent::Finished);
        assert_eq!(
            *delivered.lock().unwrap(),
            vec!["\"/workspaces/subscribed-first\""]
        );
    }

    #[test]
    fn holds_a_click_made_while_the_window_is_replaced_until_the_new_page_is_ready() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();
        let delivered = Arc::new(Mutex::new(Vec::new()));
        let delivered_clone = delivered.clone();
        handle.listen("agentrq:navigate", move |event| {
            delivered_clone
                .lock()
                .unwrap()
                .push(event.payload().to_string());
        });
        on_page_load(&handle, &PageLoadEvent::Finished);
        navigation_ready(&handle);

        // A profile switch or a reload: the old page is still the last one
        // that loaded, but it is going.
        page_leaving(&handle);
        state.navigate(&handle, "/workspaces/w1/tasks/t1");
        assert!(delivered.lock().unwrap().is_empty());

        // The new page loads, then subscribes.
        on_page_load(&handle, &PageLoadEvent::Started);
        on_page_load(&handle, &PageLoadEvent::Finished);
        assert!(delivered.lock().unwrap().is_empty());
        navigation_ready(&handle);
        on_page_load(&handle, &PageLoadEvent::Finished);
        navigation_ready(&handle);
        assert_eq!(
            *delivered.lock().unwrap(),
            vec!["\"/workspaces/w1/tasks/t1\""]
        );
    }

    #[test]
    fn shows_the_newest_count_whatever_order_the_counts_arrive_in() {
        let (_app, handle) = test_app();
        init(&handle).unwrap();
        let state = handle.state::<ShellState<tauri::test::MockRuntime>>();
        let desk = Arc::new(FakeDesk::default());
        let dir = tempfile::tempdir().unwrap();
        let notifier = Notifier::new(
            Arc::new(crate::network::tests::network(dir.path(), "")),
            desk.clone(),
        );
        // A reply counts 1 and focus clears it, but the 1 is shown last.
        notifier.on_event(&reply_event("t1", "agent", "m1"));
        notifier.clear_unread();
        let counts = desk.unread.lock().unwrap().clone();
        let generations = desk.generations.lock().unwrap().clone();
        for (count, generation) in counts.into_iter().zip(generations).rev() {
            state.set_unread_count(&handle, count.into(), generation);
        }
        assert_eq!(state.unread_count(), 0);
    }
}
