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
mod panic_log;
mod scheme;

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::Duration;

use agentrq_desktop_core::updater::{
    AutoUpdater, PublishedStatus, StdInstallerSpawner, Updater, UpdaterDeps, UpdaterEvent,
};
use scheme::Route;
use tauri::{AppHandle, Emitter, Manager, Runtime, Url};
use tauri_plugin_updater::UpdaterExt;

/// The renderer's origin: `app://localhost`, which WebView2 spells
/// `http://app.localhost`.
fn app_url() -> Url {
    let url = if cfg!(windows) {
        "http://app.localhost/"
    } else {
        "app://localhost/"
    };
    Url::parse(url).expect("static url")
}

/// The Vite dev server, when this is `tauri dev`. The scheme forwards to it,
/// so dev and release share one origin.
fn dev_url<R: Runtime>(app: &AppHandle<R>) -> Option<Url> {
    if tauri::is_dev() {
        app.config().build.dev_url.clone()
    } else {
        None
    }
}

async fn handle<R: Runtime>(
    app: &AppHandle<R>,
    req: http::Request<Vec<u8>>,
) -> http::Response<Vec<u8>> {
    match scheme::route(req.uri().path()) {
        Route::Proxy => scheme::proxy_pending(),
        Route::Asset(path) => match dev_url(app) {
            Some(dev) => from_dev_server(&dev, &req).await,
            None => scheme::asset(
                app.asset_resolver()
                    .get(path)
                    .map(|a| (a.bytes, a.mime_type)),
            ),
        },
    }
}

/// Fetches a renderer file from Vite. The deadline is a race, never only an
/// abort (docs/agents/desktop.md).
async fn from_dev_server(dev: &Url, req: &http::Request<Vec<u8>>) -> http::Response<Vec<u8>> {
    static CLIENT: OnceLock<reqwest::Client> = OnceLock::new();
    let client = CLIENT.get_or_init(reqwest::Client::new);
    let path = req.uri().path_and_query().map_or("/", |p| p.as_str());
    let Ok(url) = dev.join(path) else {
        return status(400, "bad path");
    };
    let fetch = async {
        let res = client.get(url).send().await?;
        let code = res.status().as_u16();
        let kind = res.headers().get("content-type").cloned();
        Ok::<_, reqwest::Error>((code, kind, res.bytes().await?))
    };
    match tokio::time::timeout(Duration::from_secs(30), fetch).await {
        Ok(Ok((code, kind, body))) => {
            let mut res = http::Response::builder()
                .status(code)
                .header("cache-control", "no-store");
            if let Some(kind) = kind {
                res = res.header("content-type", kind.as_bytes());
            }
            res.body(body.to_vec())
                .unwrap_or_else(|_| status(502, "bad answer from vite"))
        }
        Ok(Err(e)) => status(502, &format!("vite is not answering: {e}")),
        Err(_) => status(504, "vite did not answer in time"),
    }
}

fn status(code: u16, text: &str) -> http::Response<Vec<u8>> {
    http::Response::builder()
        .status(code)
        .body(text.as_bytes().to_vec())
        .expect("static response")
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

pub fn run() {
    let app = chrome::plugins(tauri::Builder::default())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .register_asynchronous_uri_scheme_protocol("app", |ctx, req, responder| {
            let app = ctx.app_handle().clone();
            tauri::async_runtime::spawn(async move {
                responder.respond(handle(&app, req).await);
            });
        })
        .invoke_handler(commands::handler())
        .setup(|app| {
            if let Ok(dir) = app.path().app_log_dir() {
                panic_log::install(dir);
            }
            init_updater(app.handle());
            app.manage(chrome::Chrome::new(app.path().app_data_dir()?));
            chrome::open_main_window(app.handle())?;
            app_menu::install(app.handle())?;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building AgentRQ Tauri");

    app.run(|app_handle, event| {
        chrome::on_run_event(app_handle, &event);
        if let tauri::RunEvent::Exit = event {
            if let Some(installer) = app_handle.try_state::<ExitInstaller>() {
                (installer.0)();
            }
        }
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
