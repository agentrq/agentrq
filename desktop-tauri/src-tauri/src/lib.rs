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

use std::sync::OnceLock;
use std::time::Duration;

use scheme::Route;
use tauri::{AppHandle, Manager, Runtime, Url};

/// The renderer's origin: `app://localhost`, which WebView2 spells
/// `http://app.localhost`.
pub(crate) fn app_url() -> Url {
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

pub fn run() {
    chrome::plugins(tauri::Builder::default())
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
            app.manage(chrome::Chrome::new(app.path().app_data_dir()?));
            chrome::open_main_window(app.handle())?;
            app_menu::install(app.handle())?;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building AgentRQ Tauri")
        .run(|app, event| chrome::on_run_event(app, &event));
}
