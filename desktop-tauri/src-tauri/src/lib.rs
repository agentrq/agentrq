// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The wiring: the app scheme, the commands, the panic hook and the window.
//! Every rule lives in a module beside this one, where it is tested; this file
//! only connects them to Tauri, so it is left out of coverage, as
//! `desktop/src/main/index.js` is.

mod app_menu;
mod chrome;
mod chrome_state;
mod commands;
mod cookie_jar;
mod event_source;
mod events;
mod ext_host;
mod ext_node;
mod extensions;
mod network;
mod notify;
mod oauth_window;
mod panel_permissions;
mod panel_scheme;
mod panic_log;
mod profiles;
mod scheme;
mod shell;
mod side_panel;
mod sign_in;
#[cfg(test)]
mod test_server;
mod upstream;
mod vault;

use std::collections::HashSet;
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Arc, Mutex};

use agentrq_desktop_core::auth::{SignInFlow, SignInWindow};
use agentrq_desktop_core::links::LinkTarget;
use agentrq_desktop_core::server_config::connection_lock;
use agentrq_desktop_core::updater::{
    AutoUpdater, PublishedStatus, StdInstallerSpawner, Updater, UpdaterDeps, UpdaterEvent,
};
use tauri::webview::{Cookie, PageLoadEvent};
use tauri::{
    AppHandle, Emitter, Manager, RunEvent, Runtime, Url, Webview, WebviewUrl, WebviewWindow,
    WebviewWindowBuilder, WindowEvent,
};
use tauri_plugin_notification::NotificationExt;
use tauri_plugin_updater::UpdaterExt;

use agentrq_desktop_core::notifications::Notification;
use event_source::Streams;
use events::EventStream;
use ext_host::ExtHost;
use extensions::{HostDrawers, PickedFolder, Picker, ShellServices};
use network::Network;
use notify::{Desk, Feed, Notifier};
use oauth_window::{HeldSignIn, SignIn};
use profiles::{app_origin, Profiles, Shell};
use scheme::{Assets, Scheme, MAIN_WEBVIEW};
use sign_in::CookieWebview;
use tauri::path::BaseDirectory;
use tauri_plugin_dialog::DialogExt;
use vault::{Keychain, KeychainError};

/// The Vite dev server, when this is `tauri dev`. The scheme forwards to it,
/// so dev and release share one origin.
fn dev_url<R: Runtime>(app: &AppHandle<R>) -> String {
    match app.config().build.dev_url.clone() {
        Some(url) if tauri::is_dev() => url.to_string(),
        _ => String::new(),
    }
}

/// The bundled renderer. Tauri's own resolver falls back to `index.html` for
/// any path, so whether a file exists is asked of the real keys.
struct Bundled<R: Runtime> {
    app: AppHandle<R>,
    keys: HashSet<String>,
}

impl<R: Runtime> Assets for Bundled<R> {
    fn exists(&self, path: &str) -> bool {
        self.keys.contains(path)
    }
    fn read(&self, path: &str) -> Option<Vec<u8>> {
        self.exists(path)
            .then(|| self.app.asset_resolver().get(path.to_string()))
            .flatten()
            .map(|asset| asset.bytes)
    }
}

/// A key in the OS keychain: the cookie jars' (`cookie-jar-key`) or the
/// extension secrets' (`extension-secrets-key`).
struct OsKeychain(&'static str);

impl OsKeychain {
    fn entry(&self) -> Result<keyring::Entry, KeychainError> {
        keyring::Entry::new("com.agentrq.desktop.tauri", self.0).map_err(keychain_error)
    }
}

/// A locked keychain, or a dismissed prompt, is a refusal this time; no store
/// at all is a missing one. macOS and Windows always have a keychain, so any
/// other failure there is passing too; on Linux it means there is no Secret
/// Service.
fn keychain_error(error: keyring::Error) -> KeychainError {
    match error {
        keyring::Error::NoStorageAccess(_) => KeychainError::Refused,
        keyring::Error::NoDefaultStore | keyring::Error::NotSupportedByStore(_) => {
            KeychainError::Missing
        }
        #[cfg(any(target_os = "macos", windows))]
        _ => KeychainError::Refused,
        #[cfg(not(any(target_os = "macos", windows)))]
        _ => KeychainError::Missing,
    }
}

impl Keychain for OsKeychain {
    fn get(&self) -> Result<Option<Vec<u8>>, KeychainError> {
        match self.entry()?.get_secret() {
            Ok(secret) => Ok(Some(secret)),
            Err(keyring::Error::NoEntry) => Ok(None),
            Err(error) => Err(keychain_error(error)),
        }
    }
    fn set(&self, secret: &[u8]) -> Result<(), KeychainError> {
        self.entry()?.set_secret(secret).map_err(keychain_error)
    }
}

type DownloadedUpdate = Arc<Mutex<Option<(tauri_plugin_updater::Update, Vec<u8>)>>>;

struct TauriAutoUpdater<R: Runtime> {
    app: AppHandle<R>,
    updater_cell: Arc<Mutex<Option<Updater>>>,
    auto_download: AtomicBool,
    auto_install_on_quit: AtomicBool,
    downloaded: DownloadedUpdate,
    checking: Arc<AtomicBool>,
}

impl<R: Runtime> TauriAutoUpdater<R> {
    fn new(
        app: AppHandle<R>,
        updater_cell: Arc<Mutex<Option<Updater>>>,
        downloaded: DownloadedUpdate,
    ) -> Self {
        Self {
            app,
            updater_cell,
            auto_download: AtomicBool::new(true),
            auto_install_on_quit: AtomicBool::new(true),
            downloaded,
            checking: Arc::new(AtomicBool::new(false)),
        }
    }
}

impl<R: Runtime> AutoUpdater for TauriAutoUpdater<R> {
    fn set_auto_download(&self, enabled: bool) {
        self.auto_download.store(enabled, Ordering::SeqCst);
    }

    fn set_auto_install_on_app_quit(&self, enabled: bool) {
        self.auto_install_on_quit.store(enabled, Ordering::SeqCst);
    }

    fn check_for_updates(&self) -> Result<(), String> {
        if self.checking.swap(true, Ordering::SeqCst) {
            return Ok(());
        }

        let app = self.app.clone();
        let updater_cell = Arc::clone(&self.updater_cell);
        let auto_download = self.auto_download.load(Ordering::SeqCst);
        let downloaded = Arc::clone(&self.downloaded);
        let checking = Arc::clone(&self.checking);

        tauri::async_runtime::spawn(async move {
            let emit_event = |ev| {
                let u = updater_cell.lock().unwrap().clone();
                if let Some(u) = u {
                    u.handle_event(ev);
                }
            };

            emit_event(UpdaterEvent::Checking);
            let updater = match app.updater() {
                Ok(u) => u,
                Err(e) => {
                    emit_event(UpdaterEvent::Error(e.to_string()));
                    checking.store(false, Ordering::SeqCst);
                    return;
                }
            };

            match updater.check().await {
                Ok(Some(update)) => {
                    let version = update.version.clone();
                    emit_event(UpdaterEvent::Available {
                        version: Some(version.clone()),
                    });
                    if auto_download {
                        let cell_for_chunk = Arc::clone(&updater_cell);
                        let mut total_downloaded: usize = 0;
                        let dl_res = update
                            .download(
                                move |chunk_len, total| {
                                    total_downloaded += chunk_len;
                                    let percent = total.map(|t| {
                                        if t > 0 {
                                            (total_downloaded as f64 / t as f64) * 100.0
                                        } else {
                                            0.0
                                        }
                                    });
                                    let u = cell_for_chunk.lock().unwrap().clone();
                                    if let Some(u) = u {
                                        u.handle_event(UpdaterEvent::DownloadProgress { percent });
                                    }
                                },
                                || {},
                            )
                            .await;

                        match dl_res {
                            Ok(bytes) => {
                                *downloaded.lock().unwrap() = Some((update, bytes));
                                emit_event(UpdaterEvent::Downloaded {
                                    version: Some(version),
                                });
                            }
                            Err(e) => {
                                emit_event(UpdaterEvent::Error(e.to_string()));
                            }
                        }
                    }
                }
                Ok(None) => {
                    emit_event(UpdaterEvent::NotAvailable);
                }
                Err(e) => {
                    emit_event(UpdaterEvent::Error(e.to_string()));
                }
            }
            checking.store(false, Ordering::SeqCst);
        });

        Ok(())
    }

    fn quit_and_install(&self) {
        if let Some((update, bytes)) = self.downloaded.lock().unwrap().take() {
            if let Err(e) = update.install(bytes) {
                eprintln!("failed to install update: {e}");
            } else {
                self.app.restart();
            }
        }
    }
}

/// Pure decision: whether an update should be installed on exit.
///
/// Only when `auto_install_on_app_quit` is enabled and an update is already downloaded.
pub fn should_install_on_exit(auto_install_on_quit: bool, has_downloaded_update: bool) -> bool {
    auto_install_on_quit && has_downloaded_update
}

impl<R: Runtime> TauriAutoUpdater<R> {
    fn install_on_exit(&self) {
        let has_download = self.downloaded.lock().unwrap().is_some();
        let auto_install = self.auto_install_on_quit.load(Ordering::SeqCst);
        if should_install_on_exit(auto_install, has_download) {
            if let Some((update, bytes)) = self.downloaded.lock().unwrap().take() {
                if let Err(e) = update.install(bytes) {
                    eprintln!("failed to install update on exit: {e}");
                }
            }
        }
    }
}

pub fn pubkey_is_set(pubkey: Option<&str>) -> bool {
    pubkey.map(|k| !k.trim().is_empty()).unwrap_or(false)
}

pub fn has_updater_pubkey<R: Runtime>(app: &AppHandle<R>) -> bool {
    let pubkey = app
        .config()
        .plugins
        .0
        .get("updater")
        .and_then(|v| v.get("pubkey"))
        .and_then(|k| k.as_str());
    pubkey_is_set(pubkey)
}

pub struct ExitInstaller(pub Arc<dyn Fn() + Send + Sync>);

fn init_updater<R: Runtime>(app: &AppHandle<R>) {
    let updater_cell = Arc::new(Mutex::new(None));
    let downloaded = Arc::new(Mutex::new(None));
    let auto_updater = Arc::new(TauriAutoUpdater::new(
        app.clone(),
        Arc::clone(&updater_cell),
        Arc::clone(&downloaded),
    ));

    let auto_for_exit = Arc::clone(&auto_updater);
    app.manage(ExitInstaller(Arc::new(move || {
        auto_for_exit.install_on_exit();
    })));

    let enabled = !tauri::is_dev() && has_updater_pubkey(app);
    let app_for_status = app.clone();
    let mut deps = UpdaterDeps::new(auto_updater, enabled, move |status: PublishedStatus| {
        let _ = app_for_status.emit("agentrq:update:status", &status);
    });

    deps.spawner = Some(Arc::new(StdInstallerSpawner));
    if let Ok(temp_dir) = app.path().temp_dir() {
        deps.create_install_log = Some(Arc::new(move || {
            agentrq_desktop_core::install_log::create_install_log(&temp_dir)
                .map(|f| Arc::new(f) as Arc<dyn agentrq_desktop_core::install_log::InstallLog>)
                .map_err(|e| e.to_string())
        }));
    }

    let updater = Updater::new(deps);
    *updater_cell.lock().unwrap() = Some(updater.clone());
    app.manage(updater.clone());
    updater.start();
}

/// Set while the main window is being replaced: its `Destroyed` event builds
/// the next one.
#[derive(Default)]
struct Reopening(AtomicBool);

/// The old main window is gone: the new one, on the active profile.
fn rebuild_main<R: Runtime>(app: &AppHandle<R>) {
    // Again: the old page may have finished a load since `reopen`.
    shell::page_leaving(app);
    app.state::<Arc<Profiles>>().main_window_closed();
    if let Err(error) = chrome::open_active_main_window(app) {
        eprintln!("[profiles] could not reopen the window: {error}");
    }
}

/// A webview's cookie store, for `sign_in`.
struct TauriCookies<R: Runtime>(Webview<R>);

impl<R: Runtime> CookieWebview for TauriCookies<R> {
    fn cookies(&self) -> Vec<Cookie<'static>> {
        self.0.cookies().unwrap_or_else(|error| {
            eprintln!("[cookies] could not read the webview's cookies: {error}");
            Vec::new()
        })
    }
    fn delete_cookie(&self, cookie: Cookie<'static>) {
        // WebView2 deletes later, which is fine: nothing on the app's origin
        // sends it.
        let _ = self.0.delete_cookie(cookie);
    }
}

/// What profiles.rs asks of the windows.
struct Windows<R: Runtime>(AppHandle<R>);

impl<R: Runtime> Shell for Windows<R> {
    /// The new window cannot be built first: it has to be `main` too, which is
    /// what the scheme and the capability know it by. Tauri frees the label
    /// and runs `run`'s callback for the old one's `Destroyed` before it counts
    /// the windows that are left, so building it there means the count never
    /// reaches zero and Linux and Windows do not quit mid-switch.
    fn reopen(&self) {
        shell::page_leaving(&self.0);
        let app = self.0.clone();
        let _ = self.0.run_on_main_thread(move || {
            let Some(webview) = chrome::main_webview(&app) else {
                return rebuild_main(&app);
            };
            chrome::save_main(&app);
            app.state::<Reopening>().0.store(true, Ordering::SeqCst);
            if webview.window().destroy().is_err() {
                app.state::<Reopening>().0.store(false, Ordering::SeqCst);
                rebuild_main(&app);
            }
        });
    }

    fn reload(&self) {
        if let Some(webview) = chrome::main_webview(&self.0) {
            let origin = app_origin(&self.0.state::<Arc<Profiles>>().active_id());
            if let Ok(url) = Url::parse(&format!("{origin}/")) {
                shell::page_leaving(&self.0);
                let _ = webview.navigate(url);
            }
        }
    }

    fn forget_store(&self, _id: &str) {
        #[cfg(target_os = "macos")]
        {
            // Its own store, and its side panel guests'.
            let app = self.0.clone();
            let stores = agentrq_desktop_core::profiles::profile_stores(_id);
            tauri::async_runtime::spawn(async move {
                for store in stores {
                    let _ = app.remove_data_store(store).await;
                }
            });
        }
    }

    fn main_webview(&self) -> Option<Arc<dyn CookieWebview>> {
        let webview = chrome::main_webview(&self.0)?;
        Some(Arc::new(TauriCookies(webview)))
    }
}

/// What notify.rs asks of the app. The Notifier owns the unread count; the
/// shell shows it, and holds a route until the main webview can take it.
struct TauriDesk<R: Runtime>(AppHandle<R>);

impl<R: Runtime> TauriDesk<R> {
    fn shell(&self) -> Option<tauri::State<'_, shell::ShellState<R>>> {
        self.0.try_state::<shell::ShellState<R>>()
    }
}

impl<R: Runtime> Desk for TauriDesk<R> {
    fn show(&self, notification: &Notification) -> bool {
        self.0
            .notification()
            .builder()
            .title(&notification.title)
            .body(&notification.body)
            .extra(notify::ROUTE_KEY, &notification.route)
            .show()
            .is_ok()
    }

    fn navigate(&self, route: &str) {
        if let Some(shell) = self.shell() {
            shell.navigate(&self.0, route);
        }
    }

    fn set_unread(&self, count: u32, generation: u64) {
        if let Some(shell) = self.shell() {
            shell.set_unread_count(&self.0, count.into(), generation);
        }
    }

    fn touch_workspace(&self, id: &str, name: &str) {
        if let Some(shell) = self.shell() {
            shell.touch_recent_workspace(&self.0, id, Some(name));
        }
    }
}

/// A removed profile's store that a quit kept from being deleted, deleted now.
#[cfg(target_os = "macos")]
fn sweep_data_stores<R: Runtime>(app: &AppHandle<R>) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let Ok(stores) = app.fetch_data_store_identifiers().await else {
            return;
        };
        for store in app.state::<Arc<Profiles>>().orphaned_stores(&stores) {
            let _ = app.remove_data_store(store).await;
        }
    });
}

/// Where a link the side panel does not keep goes: the system browser for
/// the web, and the app's own sign-in window for AgentRQ's.
fn open_link<R: Runtime>(app: &AppHandle<R>, target: LinkTarget, url: &str) {
    match target {
        LinkTarget::SignIn => open_sign_in(app, url.to_string()),
        _ => {
            let _ = tauri_plugin_opener::open_url(url, None::<&str>);
        }
    }
}

/// Sign-in windows are numbered: one closing must never free a label another
/// still uses. None of them is `main`, so none gets a capability.
static SIGN_INS: AtomicU32 = AtomicU32::new(1);

type Flow = Arc<Mutex<Option<SignInFlow>>>;

fn lock_flow(flow: &Flow) -> std::sync::MutexGuard<'_, Option<SignInFlow>> {
    flow.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

struct SignInWebview<'a, R: Runtime>(&'a WebviewWindow<R>);

impl<R: Runtime> SignInWindow for SignInWebview<'_, R> {
    fn load_url(&mut self, url: &str) {
        if let Ok(url) = Url::parse(url) {
            let _ = self.0.navigate(url);
        }
    }
    fn is_destroyed(&self) -> bool {
        self.0.app_handle().get_webview(self.0.label()).is_none()
    }
    fn close(&mut self) {
        let _ = self.0.close();
    }
}

/// Sign the active profile in, in a window on the real server and on that
/// profile's webview storage. Never the system browser: the cookie would land
/// in a jar the app cannot read. It waits for an authorisation window on that
/// store, and holds others off while it is open (`oauth_window::open_sign_in`).
pub(crate) fn open_sign_in<R: Runtime>(app: &AppHandle<R>, start_url: String) {
    oauth_window::open_sign_in(app, start_url, Box::new(open_sign_in_window));
}

fn open_sign_in_window<R: Runtime>(app: &AppHandle<R>, start_url: String, held: SignIn) {
    let profiles = app.state::<Arc<Profiles>>().inner().clone();
    let id = held.profile().to_string();
    // The held profile's own server, not the active one's: a switch may
    // land between the check and here.
    let Some(server) = profiles.server_of_profile(&id) else {
        return;
    };
    let label = format!("sign-in-{}", SIGN_INS.fetch_add(1, Ordering::Relaxed));
    let flow: Flow = Arc::default();
    let on_load = flow.clone();
    let profile = id.clone();
    let blank = Url::parse("about:blank").expect("static url");
    let builder = WebviewWindowBuilder::new(app, &label, WebviewUrl::External(blank))
        .title("Sign in to AgentRQ")
        .inner_size(520.0, 720.0)
        // Back on the server outside the auth endpoints: the callback has set
        // the cookie. The jar is read off this thread (wry#583).
        .on_page_load(move |window, payload| {
            if payload.event() != PageLoadEvent::Finished {
                return;
            }
            let done = lock_flow(&on_load)
                .as_mut()
                .is_some_and(|flow| flow.on_navigation(payload.url().as_str()));
            if done {
                tauri::async_runtime::spawn(finish_sign_in(
                    window,
                    on_load.clone(),
                    profile.clone(),
                ));
            }
        });
    let window = match chrome::isolate(builder, &profiles, &id).build() {
        Ok(window) => window,
        Err(error) => {
            eprintln!("[sign-in] could not open the window: {error}");
            return;
        }
    };
    // Closing it before it is done is giving up.
    let on_close = flow.clone();
    let held = HeldSignIn::new(held);
    window.on_window_event(move |event| {
        held.on_window_event(event);
        if let WindowEvent::Destroyed = event {
            if let Some(flow) = lock_flow(&on_close).as_mut() {
                flow.on_closed();
            }
        }
    });
    *lock_flow(&flow) = Some(SignInFlow::start(
        &server,
        &start_url,
        &mut SignInWebview(&window),
    ));
}

async fn finish_sign_in<R: Runtime>(window: WebviewWindow<R>, flow: Flow, id: String) {
    let profiles = window.state::<Arc<Profiles>>().inner().clone();
    let cookies = Arc::new(TauriCookies(window.as_ref().clone()));
    let signed_in = profiles.finish_sign_in(&id, cookies).await;
    if let Some(flow) = lock_flow(&flow).as_mut() {
        flow.finish(signed_in, &mut SignInWebview(&window));
    }
}

/// The extension host: the bundled entry, run by the Node the Extensions page
/// downloads into the app data dir. It starts only once an installed
/// extension is enabled and that Node is there.
fn extension_host<R: Runtime>(
    app: &AppHandle<R>,
    network: Arc<Network>,
    data_dir: std::path::PathBuf,
) -> tauri::Result<Arc<ExtHost>> {
    let node = ext_node::Node::new(&data_dir);
    // What a quit mid-download left behind.
    node.sweep();
    let entry = app
        .path()
        .resolve("ext-host/agentrq-ext-host.mjs", BaseDirectory::Resource)?;
    let args = vec![
        entry.into(),
        data_dir.clone().into(),
        app.package_info().version.to_string().into(),
    ];
    let (emitter, authorizer) = (app.clone(), app.clone());
    let services = ShellServices::new(
        network,
        Arc::new(OsKeychain("extension-secrets-key")),
        Box::new(move |method, params| match method {
            "changed" => {
                let _ = emitter.emit("agentrq:extensions:changed", params);
            }
            _ => side_panel::host_told(&emitter, method, params),
        }),
        Box::new(move |url, redirect| {
            Box::pin(oauth_window::for_host(authorizer.clone(), url, redirect))
        }),
    );
    let host = ExtHost::on_node(node, args, data_dir, Arc::new(services));
    app.manage(host.clone());
    Ok(host)
}

/// `chooseFolder`'s dialog, off the async runtime: it blocks until closed.
fn pick_folder<R: Runtime>(window: WebviewWindow<R>) -> PickedFolder {
    Box::pin(async move {
        let picked = tauri::async_runtime::spawn_blocking(move || {
            window
                .dialog()
                .file()
                .set_title("Choose an extension folder")
                .set_parent(&window)
                .blocking_pick_folder()
        })
        .await;
        picked
            .ok()
            .flatten()
            .and_then(|path| path.into_path().ok())
            .map(|path| path.to_string_lossy().into_owned())
    })
}

pub fn run() {
    let app = chrome::plugins(tauri::Builder::default())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(notify::ShellOnly(tauri_plugin_notification::init()))
        .register_asynchronous_uri_scheme_protocol("app", |ctx, request, responder| {
            let app = ctx.app_handle().clone();
            let webview = ctx.webview_label().to_string();
            tauri::async_runtime::spawn(async move {
                let scheme = app.state::<Scheme>();
                responder.respond(scheme.handle(&webview, request).await);
            });
        })
        // An Extension's pages in the side panel, and their bridge. Only a
        // panel guest is answered (`panel_scheme.rs`).
        .register_asynchronous_uri_scheme_protocol("agentrq-ext", |ctx, request, responder| {
            let app = ctx.app_handle().clone();
            let label = ctx.webview_label().to_string();
            tauri::async_runtime::spawn(async move {
                let top = app
                    .get_webview(&label)
                    .and_then(|webview| webview.url().ok())
                    .map(|url| url.to_string());
                let token = side_panel::token(&app, &label);
                let scheme = app.state::<panel_scheme::PanelScheme>();
                let page = panel_scheme::Page {
                    top_url: top.as_deref(),
                    token: token.as_deref(),
                };
                responder.respond(scheme.handle(&label, page, &request).await);
            });
        })
        // Tauri would serve its own assets on every webview, a guest's too:
        // the app's window loads its page through `app` instead, so nothing
        // is served here.
        .register_uri_scheme_protocol("tauri", |_, _| panel_scheme::app_internal())
        .invoke_handler(commands::handler())
        .setup(|app| {
            if let Ok(dir) = app.path().app_log_dir() {
                panic_log::install(dir);
            }
            let data_dir = app.path().app_data_dir()?;
            let lock = connection_lock(std::env::var("AGENTRQ_SERVER_URL").ok().as_deref());
            let network = Arc::new(Network::new(
                data_dir.clone(),
                Arc::new(OsKeychain("cookie-jar-key")),
                profiles::first_profile(&data_dir, lock.as_deref()),
            ));
            let handle = app.handle().clone();
            let notifier = Arc::new(Notifier::new(
                network.clone(),
                Arc::new(TauriDesk(handle.clone())),
            ));
            let host = extension_host(&handle, network.clone(), data_dir.clone())?;
            let events = Arc::new(events::ToolReviewEvents::new(
                notifier.clone(),
                host.clone(),
            ));
            let feed = Arc::new(Feed {
                stream: EventStream::spawn(network.clone(), events),
                notifier: notifier.clone(),
            });
            let profiles = Arc::new(Profiles::new(
                data_dir.clone(),
                lock,
                network.clone(),
                Box::new(Windows(handle.clone())),
                feed,
            ));
            profiles.sweep();
            profiles.watch();
            app.manage(profiles.auth_windows());
            app.manage(profiles.clone());
            let clicked = notifier.clone();
            let site = handle.clone();
            handle.notification().on_action(move |action| {
                let extra = action.notification().map(|n| n.extra());
                if let Some(route) = notify::tapped(action.action_id(), extra) {
                    clicked.clicked(&route);
                } else if let Some((label, id)) = notify::site_tapped(action.action_id(), extra) {
                    side_panel::site_notification_clicked(&site, &label, id);
                }
            })?;
            app.manage(notifier.clone());
            app.manage(Reopening::default());
            #[cfg(target_os = "macos")]
            sweep_data_stores(&handle);
            let keys = handle
                .asset_resolver()
                .iter()
                .map(|(key, _)| key.to_string())
                .collect();
            let assets = Box::new(Bundled {
                app: handle.clone(),
                keys,
            });
            app.manage(network.clone());
            app.manage(Streams::default());
            app.manage(Picker::<tauri::Wry>(Box::new(pick_folder)));
            let drawers = HostDrawers {
                host: host.clone(),
                origin: Box::new(move || app_origin(&profiles.active_id())),
            };
            let scheme = Scheme::new(network, assets, &dev_url(&handle))
                .drawers(Box::new(drawers))
                .on_proxied(Box::new(move |method, path| notifier.proxied(method, path)));
            app.manage(scheme);
            init_updater(&handle);
            let side_panel = side_panel::SidePanelState::new(&handle, Box::new(open_link));
            app.manage(side_panel::scheme(host.clone()));
            app.manage(side_panel);
            app.manage(panel_permissions::PanelPermissions::default());
            app.manage(chrome::Chrome::new(data_dir));
            shell::init(&handle)?;
            chrome::open_active_main_window(&handle)?;
            app_menu::install(&handle)?;
            let _ = app_menu::install_tray(&handle);
            chrome::install_hooks(&handle);
            // Off the async runtime: the first check of Node hashes it.
            tauri::async_runtime::spawn_blocking(move || host.boot());
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building AgentRQ Tauri");

    app.run(|app, event| {
        match &event {
            RunEvent::WindowEvent {
                label,
                event: WindowEvent::Destroyed,
                ..
            } if label == MAIN_WEBVIEW
                && app.state::<Reopening>().0.swap(false, Ordering::SeqCst) =>
            {
                rebuild_main(app);
            }
            RunEvent::Exit => {
                let host = app.state::<Arc<ExtHost>>().inner().clone();
                host.kill_sync();
                if let Some(installer) = app.try_state::<ExitInstaller>() {
                    (installer.0)();
                }
            }
            _ => {}
        }
        chrome::on_run_event(app, &event);
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decides_whether_to_install_on_exit() {
        assert!(should_install_on_exit(true, true));
        assert!(!should_install_on_exit(false, true));
        assert!(!should_install_on_exit(true, false));
        assert!(!should_install_on_exit(false, false));
    }

    #[test]
    fn detects_whether_pubkey_is_set() {
        assert!(!pubkey_is_set(None));
        assert!(!pubkey_is_set(Some("")));
        assert!(!pubkey_is_set(Some("   ")));
        assert!(!pubkey_is_set(Some("\t\n")));
        assert!(pubkey_is_set(Some("dW50cnVzdGVkIGNvbW1lbnQ6...")));
    }
}
