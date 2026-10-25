// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! WP6a shell features: link routing, deep links, single instance, global shortcut,
//! badge count, and tray menu.
//!
//! Pure logic lives in `agentrq_desktop_core` (`links`, `deep_link`, `tray`, `notifications`, `auth`).
//! This module connects that pure core to Tauri's plugins and window lifecycle.

use std::sync::atomic::{AtomicI64, Ordering};
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
use tauri::{AppHandle, Emitter, Listener, Manager, Runtime, Url, WindowEvent};
use tauri_plugin_deep_link::DeepLinkExt;
use tauri_plugin_global_shortcut::ShortcutState;
use tauri_plugin_opener::OpenerExt;

/// State for the desktop shell (unread badges, recent workspaces, deep link pending route).
pub struct ShellState<R: Runtime = tauri::Wry> {
    pub(crate) pending_route: Mutex<PendingRoute>,
    pub(crate) unread_count: AtomicI64,
    pub(crate) recent_workspaces: Mutex<RecentWorkspaces>,
    _runtime: std::marker::PhantomData<fn() -> R>,
}

impl<R: Runtime> Default for ShellState<R> {
    fn default() -> Self {
        Self {
            pending_route: Mutex::new(PendingRoute::new()),
            unread_count: AtomicI64::new(0),
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
            PageLoadEvent::Started => {
                let mut guard = self.pending_route.lock().unwrap();
                guard.on_page_load_started();
            }
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

    /// Update the unread count, refreshing the tray and badge.
    #[allow(dead_code)]
    pub fn set_unread_count(&self, app: &AppHandle<R>, count: i64) {
        self.unread_count.store(count, Ordering::SeqCst);
        self.refresh_tray(app);
        self.set_badge_count(app, count);
    }

    /// Clear unread count, updating badge and tray.
    pub fn clear_unread(&self, app: &AppHandle<R>) {
        self.set_unread_count(app, 0);
    }

    /// Set the application badge count (macOS/Linux: dock badge, Windows: taskbar overlay).
    pub fn set_badge_count(&self, app: &AppHandle<R>, count: i64) {
        let state = badge_for(count, PLATFORM);
        if let Some(win) = app.get_window("main") {
            #[cfg(target_os = "windows")]
            {
                if let Some(_overlay) = state.overlay {
                    let img = Image::new_owned(vec![0u8; 16 * 16 * 4], 16, 16);
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
    #[allow(dead_code)]
    pub fn touch_recent_workspace(&self, app: &AppHandle<R>, id: &str, name: Option<&str>) {
        self.recent_workspaces.lock().unwrap().touch(id, name);
        self.refresh_tray(app);
    }

    /// Re-render the tray menu and tooltip with the latest unread count and recent workspaces.
    pub fn refresh_tray(&self, app: &AppHandle<R>) {
        let unread = self.unread_count.load(Ordering::SeqCst);
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
        let count = state.unread_count.load(Ordering::SeqCst);
        state.set_badge_count(app, count);
    }
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
            let unread = state.unread_count.load(Ordering::SeqCst);
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
                    #[cfg(not(test))]
                    self.app.exit(0);
                    #[cfg(test)]
                    let _ = self.app;
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

/// Handle window focus events for badge and unread clearing.
pub(crate) fn handle_window_event<R: Runtime>(window: &tauri::Window<R>, event: &WindowEvent) {
    if window.label() == "main" {
        if let WindowEvent::Focused(true) = event {
            if let Some(state) = window.app_handle().try_state::<ShellState<R>>() {
                state.clear_unread(window.app_handle());
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

/// Handle deep link open URLs.
pub(crate) fn handle_open_urls<R: Runtime>(app: &AppHandle<R>, urls: &[Url]) {
    if let Some(state) = app.try_state::<ShellState<R>>() {
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

/// Initialize shell services: state management, navigate readiness listener, and startup deep link.
pub fn init<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    app.manage(ShellState::<R>::default());

    // Listen for renderer readiness to deliver held routes
    let app_ready = app.clone();
    app.listen("agentrq:navigate:ready", move |_| {
        if let Some(state) = app_ready.try_state::<ShellState<R>>() {
            state.on_subscribed(&app_ready);
        }
    });

    // First-launch argv deep link
    let args: Vec<String> = std::env::args().collect();
    let current_urls = app.deep_link().get_current().ok().flatten();
    check_first_launch_route(app, &args, current_urls);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use tauri::Listener;

    fn test_app() -> (
        tauri::App<tauri::test::MockRuntime>,
        tauri::AppHandle<tauri::test::MockRuntime>,
    ) {
        let mut ctx = tauri::test::mock_context(tauri::test::noop_assets());
        ctx.config_mut().identifier = "com.agentrq.desktop.tauri".to_string();
        let app = crate::chrome::plugins(tauri::test::mock_builder())
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

        // 2. Finished delivery
        on_page_load(&handle, &PageLoadEvent::Finished);
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/1\""]);

        // 3. Renderer subscription signal re-emits if waiting
        delivered.lock().unwrap().clear();
        state.on_subscribed(&handle);
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/1\""]);

        // 4. Once subscribed and page loaded, subsequent navigate delivers immediately
        delivered.lock().unwrap().clear();
        state.navigate(&handle, "/workspaces/2");
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/2\""]);

        // 5. Started resets
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
        state.set_unread_count(&handle, 1);
        reapply_badge(&handle);

        // Window focus, unread count, badge and window event
        let win = tauri::WebviewWindowBuilder::new(&handle, "main", tauri::WebviewUrl::default())
            .build()
            .unwrap();
        let _ = win.minimize();
        assert!(focus_main_window(&handle).is_some());

        state.set_unread_count(&handle, 5);
        assert_eq!(state.unread_count.load(Ordering::SeqCst), 5);
        reapply_badge(&handle);

        handle_window_event(&win.as_ref().window(), &WindowEvent::Focused(true));
        assert_eq!(state.unread_count.load(Ordering::SeqCst), 0);
        handle_window_event(&win.as_ref().window(), &WindowEvent::Focused(false));

        // Readiness listener in init
        let _ = handle.emit("agentrq:navigate:ready", ());

        // Open deep link URLs callback
        delivered.lock().unwrap().clear();
        handle_open_urls(
            &handle,
            &[Url::parse("agentrq-tauri://workspaces/ws_open").unwrap()],
        );
        assert_eq!(*delivered.lock().unwrap(), vec!["\"/workspaces/ws_open\""]);

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
        handle_open_urls(
            &handle,
            &[Url::parse("agentrq-tauri://workspaces/1").unwrap()],
        );
        check_first_launch_route(&handle, &["app".into()], None);
        handle_tray_menu_event(&handle, "open");
        on_page_load(&handle, &PageLoadEvent::Started);
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
        std::thread::sleep(std::time::Duration::from_millis(50));
        assert!(handle.get_window("sign-in-1").is_some());
    }
}
