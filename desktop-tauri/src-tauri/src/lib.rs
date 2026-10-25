// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The wiring: the app scheme, the commands, the panic hook and the window.
//! Every rule lives in a module beside this one, where it is tested; this file
//! only connects them to Tauri, so it is left out of coverage, as
//! `desktop/src/main/index.js` is.

mod commands;
mod cookie_jar;
mod event_source;
mod network;
mod panic_log;
mod scheme;
#[cfg(test)]
mod test_server;
mod upstream;
mod vault;

use std::collections::HashSet;
use std::io;
use std::path::PathBuf;
use std::sync::Arc;

use agentrq_desktop_core::server_config::{
    connection_lock, ConfigFile, ServerConfigStore, CONFIG_FILENAME,
};
use tauri::webview::PageLoadEvent;
use tauri::{AppHandle, Manager, Runtime, Url, WebviewUrl, WebviewWindowBuilder};

use event_source::Streams;
use network::Network;
use scheme::{Assets, Scheme, MAIN_WEBVIEW};
use vault::{Keychain, KeychainError};

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

/// The key for the cookie jars, in the OS keychain.
struct OsKeychain;

impl OsKeychain {
    fn entry() -> Result<keyring::Entry, KeychainError> {
        keyring::Entry::new("com.agentrq.desktop.tauri", "cookie-jar-key").map_err(keychain_error)
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
        match Self::entry()?.get_secret() {
            Ok(secret) => Ok(Some(secret)),
            Err(keyring::Error::NoEntry) => Ok(None),
            Err(error) => Err(keychain_error(error)),
        }
    }
    fn set(&self, secret: &[u8]) -> Result<(), KeychainError> {
        Self::entry()?.set_secret(secret).map_err(keychain_error)
    }
}

/// `agentrq-desktop.json`, as Electron keeps it.
struct ConfigOnDisk(PathBuf);

impl ConfigFile for ConfigOnDisk {
    fn read_file(&self) -> io::Result<String> {
        std::fs::read_to_string(&self.0)
    }
    fn write_file(&self, contents: &str) -> io::Result<()> {
        vault::write_private(&self.0, contents.as_bytes())
    }
}

/// The profile to start on: the stored active one, pointed at
/// `AGENTRQ_SERVER_URL` when that pins the server. WP4 owns switching.
fn first_profile(data_dir: &std::path::Path) -> (String, String) {
    let store = ServerConfigStore::new(ConfigOnDisk(data_dir.join(CONFIG_FILENAME)));
    let active = store.active();
    let pinned = connection_lock(std::env::var("AGENTRQ_SERVER_URL").ok().as_deref());
    (active.id, pinned.unwrap_or(active.server_url))
}

fn main_window<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    WebviewWindowBuilder::new(app, MAIN_WEBVIEW, WebviewUrl::CustomProtocol(app_url()))
        // A reload or a navigation leaves the old page's event streams running.
        .on_page_load(|webview, payload| {
            if payload.event() == PageLoadEvent::Started {
                webview.state::<Streams>().close_all();
            }
        })
        .title("AgentRQ Tauri")
        .inner_size(1280.0, 820.0)
        .min_inner_size(940.0, 600.0)
        .build()?;
    Ok(())
}

pub fn run() {
    tauri::Builder::default()
        .register_asynchronous_uri_scheme_protocol("app", |ctx, request, responder| {
            let app = ctx.app_handle().clone();
            let webview = ctx.webview_label().to_string();
            tauri::async_runtime::spawn(async move {
                let scheme = app.state::<Scheme>();
                responder.respond(scheme.handle(&webview, request).await);
            });
        })
        .invoke_handler(commands::handler())
        .setup(|app| {
            if let Ok(dir) = app.path().app_log_dir() {
                panic_log::install(dir);
            }
            let data_dir = app.path().app_data_dir()?;
            let network = Arc::new(Network::new(
                data_dir.clone(),
                Arc::new(OsKeychain),
                first_profile(&data_dir),
            ));
            let handle = app.handle().clone();
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
            app.manage(Scheme::new(network, assets, &dev_url(&handle)));
            main_window(&handle)?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running AgentRQ Tauri");
}
