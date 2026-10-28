// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! `window.agentrq.extensions`, and what the extension host asks of the shell.
//!
//! Each command is Electron's `agentrq:extensions:*` handler, answered by the
//! extension host (`ext_host.rs`). The host in turn asks the shell for what
//! only the shell has: a fetch through the profile's jar, and sealed secrets.

use std::future::Future;
use std::path::PathBuf;
use std::pin::Pin;
use std::sync::Arc;

use agentrq_desktop_core::ext_host::fetch_allowed;
use agentrq_desktop_core::ext_node::needs_node as needs_node_reason;
use agentrq_desktop_core::ext_rpc;
use agentrq_desktop_core::protocol::{
    drawer_lookup, filter_request_headers, filter_response_headers, proxy_error_reply, sends_body,
    DrawerLookup, ProxyError,
};
use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use serde_json::{json, Value};
use tauri::{AppHandle, Emitter, Runtime, State, Window};

use crate::ext_host::{Answer, ExtHost, Services};
use crate::ext_node::{Failure, Health};
use crate::network::Network;
use crate::scheme::Drawers;
use crate::upstream::{self, pairs, BodyLimits, Outgoing};
use crate::vault::{load_key, Vault};

/// What a value the shell sealed for the host starts with (`vault.js`).
pub const SEALED_PREFIX: &str = "arqv1:";

/// The supervisor's authorisation window, given its page and the redirect
/// it ends at (`oauth_window::for_host`).
pub type AuthWindow = Box<dyn Fn(String, String) -> Answer + Send + Sync>;

/// Where the host's news goes: its notification and what it carries.
pub type Told = Box<dyn Fn(&str, &Value) + Send + Sync>;

/// What the host is answered with, and where its news goes.
pub struct ShellServices(Arc<Shell>);

struct Shell {
    network: Arc<Network>,
    /// The secrets' key file, separate from the cookie jars'.
    key_path: PathBuf,
    /// Held while the key is read, so two asks never each make one.
    vault: tokio::sync::Mutex<Option<Vault>>,
    on_told: Told,
    auth_window: AuthWindow,
}

impl ShellServices {
    pub fn new(
        network: Arc<Network>,
        key_path: PathBuf,
        on_told: Told,
        auth_window: AuthWindow,
    ) -> Self {
        Self(Arc::new(Shell {
            network,
            key_path,
            vault: tokio::sync::Mutex::new(None),
            on_told,
            auth_window,
        }))
    }
}

impl Shell {
    /// The secrets' vault, cached once its key is read; asked again next time
    /// when the key file could not be written.
    async fn vault(&self) -> Option<Vault> {
        let mut cache = self.vault.lock().await;
        if cache.is_none() {
            let path = self.key_path.clone();
            if let Ok(Ok(key)) = tokio::task::spawn_blocking(move || load_key(&path)).await {
                *cache = Some(Vault::new(&key));
            }
        }
        cache.clone()
    }

    async fn seal(&self, params: Value) -> Result<Value, String> {
        let Some(vault) = self.vault().await else {
            return Ok(
                json!({ "ok": false, "reason": "This system has no secure storage available." }),
            );
        };
        let sealed: Vec<String> = strings(&params["texts"])
            .iter()
            .map(|text| {
                format!(
                    "{SEALED_PREFIX}{}",
                    STANDARD.encode(vault.seal(text.as_bytes()))
                )
            })
            .collect();
        Ok(json!({ "ok": true, "sealed": sealed }))
    }

    async fn open(&self, params: Value) -> Result<Value, String> {
        let vault = self.vault().await;
        let opened: Vec<Option<String>> = strings(&params["blobs"])
            .iter()
            .map(|blob| {
                let sealed = STANDARD.decode(blob.strip_prefix(SEALED_PREFIX)?).ok()?;
                String::from_utf8(vault.as_ref()?.open(&sealed)?).ok()
            })
            .collect();
        Ok(json!({ "opened": opened }))
    }

    /// Electron's `profileFetch`, on the profile's own server only.
    async fn profile_fetch(&self, params: Value) -> Result<Value, String> {
        let url = params["url"].as_str().unwrap_or_default().to_string();
        if !fetch_allowed(&url, &self.network.server_url()) {
            return Ok(refused(
                "Extensions reach only this profile's AgentRQ server.",
            ));
        }
        let method = params["method"].as_str().unwrap_or("GET");
        let Ok(method) = http::Method::from_bytes(method.as_bytes()) else {
            return Ok(refused("That is not an HTTP method."));
        };
        let body = match params["body"].as_str().map(|body| STANDARD.decode(body)) {
            Some(Ok(body)) => Some(body),
            Some(Err(_)) => return Ok(refused("The request body was not base64.")),
            None => None,
        };
        let headers: Vec<(&str, &str)> = params["headers"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|pair| Some((pair[0].as_str()?, pair[1].as_str()?)))
            .collect();
        let has_body = sends_body(method.as_str(), body.is_some());
        let net = self.network.active().await;
        let out = Outgoing {
            timeout_ms: net.protocol("").timeout_ms(has_body),
            method,
            url,
            headers: filter_request_headers(headers),
            body: body.filter(|_| has_body),
            limits: BodyLimits {
                max_bytes: FETCH_MAX_BODY_BYTES,
                ..BodyLimits::default()
            },
        };
        let incoming = match upstream::send(&net.client, out).await {
            Ok(incoming) => incoming,
            Err(error) => return Ok(refused(&reason_for(&error))),
        };
        Ok(match incoming.body {
            Ok(body) => json!({
                "ok": true,
                "status": incoming.status,
                "headers": filter_response_headers(pairs(&incoming.headers)),
                "body": STANDARD.encode(body),
            }),
            Err(error) => refused(&reason_for(&error)),
        })
    }
}

/// The largest body an extension's fetch can be answered with: base64 on one
/// wire line, which the host drops whole when it is longer than
/// [`ext_rpc::MAX_LINE_BYTES`], leaving the fetch to wait out its deadline.
const FETCH_MAX_BODY_BYTES: u64 = (ext_rpc::MAX_LINE_BYTES as u64 / 4) * 3 - 1024 * 1024;

fn strings(value: &Value) -> Vec<String> {
    value
        .as_array()
        .into_iter()
        .flatten()
        .map(|item| item.as_str().unwrap_or_default().to_string())
        .collect()
}

fn refused(reason: &str) -> Value {
    json!({ "ok": false, "reason": reason })
}

/// The proxy's own words for why a request failed.
fn reason_for(error: &ProxyError) -> String {
    let reply = proxy_error_reply(error);
    serde_json::from_str::<Value>(&reply.body)
        .ok()
        .and_then(|body| body["error"].as_str().map(str::to_string))
        .unwrap_or(reply.body)
}

impl Services for ShellServices {
    fn serve(&self, method: &str, params: Value) -> Option<Answer> {
        let this = self.0.clone();
        Some(match method {
            "profileFetch" => Box::pin(async move { this.profile_fetch(params).await }),
            "vault" => {
                Box::pin(async move { Ok(json!({ "available": this.vault().await.is_some() })) })
            }
            "seal" => Box::pin(async move { this.seal(params).await }),
            "open" => Box::pin(async move { this.open(params).await }),
            "authWindow" => {
                let text = |key: &str| params[key].as_str().unwrap_or_default().to_string();
                (this.auth_window)(text("url"), text("redirectUri"))
            }
            _ => return None,
        })
    }

    fn changed(&self, name: &str, reason: &str) {
        (self.0.on_told)("changed", &json!({ "name": name, "reason": reason }));
    }

    fn server_url(&self) -> String {
        self.0.network.server_url()
    }

    fn told(&self, method: &str, params: &Value) {
        (self.0.on_told)(method, params);
    }
}

/// The folder `chooseFolder` picked; `None` when the dialog was dismissed.
pub type PickedFolder = Pin<Box<dyn Future<Output = Option<String>> + Send>>;

/// Opens the dialog for `chooseFolder`, over the window that asked.
pub type FolderPicker<R> = Box<dyn Fn(Window<R>) -> PickedFolder + Send + Sync>;

/// The managed picker.
pub struct Picker<R: Runtime>(pub FolderPicker<R>);

/// Drawers, from the extension host, on the active profile's origin.
pub struct HostDrawers {
    pub host: Arc<ExtHost>,
    pub origin: Box<dyn Fn() -> String + Send + Sync>,
}

impl Drawers for HostDrawers {
    fn origin(&self) -> String {
        (self.origin)()
    }

    fn code(
        &self,
        format: Option<String>,
    ) -> Pin<Box<dyn Future<Output = DrawerLookup> + Send + '_>> {
        Box::pin(async move {
            match self
                .host
                .call("drawerFor", json!({ "format": format }))
                .await
            {
                Ok(answer) => drawer_lookup(&answer),
                Err(reason) => DrawerLookup::Refused {
                    reason: Some(reason),
                },
            }
        })
    }
}

/// What arrives as an object: a grant, settings, a context or a target.
type Object = serde_json::Map<String, Value>;

/// Where the Node download's progress goes: an event of its own, since
/// `agentrq:extensions:changed` makes the page reload everything.
pub const NODE_PROGRESS: &str = "agentrq:extensions:node-progress";

/// `agentrq:extensions:state`, with what the page says about Node. Without
/// the pinned Node the host cannot run, so the page is read from disk.
#[tauri::command]
pub async fn extensions_state(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    let Some(node) = host.node().cloned() else {
        return host.call("state", json!({})).await;
    };
    let health = node.checked().await;
    let mut state = match health {
        Health::Ready(_) => host.call("state", json!({})).await?,
        _ => host.state_from_disk(),
    };
    if let Value::Object(map) = &mut state {
        map.insert("node".into(), node.status(&health));
    }
    Ok(state)
}

/// Why a call that needs the host cannot be made yet, if it cannot: Node is
/// downloaded only from `extensions_download_node`, never on the way.
async fn needs_node(host: &ExtHost) -> Option<Value> {
    let node = host.node()?;
    match node.checked().await {
        Health::Ready(_) => None,
        _ => Some(json!({ "ok": false, "needsNode": true, "reason": needs_node_reason() })),
    }
}

/// `agentrq:extensions:refresh`
#[tauri::command]
pub async fn extensions_refresh(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    if let Some(refusal) = needs_node(&host).await {
        return Ok(refusal);
    }
    host.call("refresh", json!({})).await
}

/// `extensions.downloadNode()`, Tauri's own: the one place Node is
/// downloaded, from the page's own button. Once it is there, the enabled
/// extensions start.
#[tauri::command]
pub async fn extensions_download_node<R: Runtime>(
    app: AppHandle<R>,
    host: State<'_, Arc<ExtHost>>,
) -> Result<Value, String> {
    let Some(node) = host.node().cloned() else {
        return Ok(json!({ "ok": true }));
    };
    let progress = move |downloaded, total, percent| {
        let detail = json!({ "downloaded": downloaded, "total": total, "percent": percent });
        let _ = app.emit(NODE_PROGRESS, detail);
    };
    Ok(match node.download(&progress).await {
        Ok(_) => {
            host.boot();
            json!({ "ok": true })
        }
        Err(Failure::Cancelled) => json!({ "ok": false, "cancelled": true }),
        Err(Failure::Failed(reason)) => json!({ "ok": false, "reason": reason }),
    })
}

/// `extensions.cancelNodeDownload()`, Tauri's own: whether one was stopped.
#[tauri::command]
pub fn extensions_cancel_node_download(host: State<'_, Arc<ExtHost>>) -> bool {
    host.node().is_some_and(|node| node.cancel())
}

/// `agentrq:extensions:choose-folder`: the dialog here, the reading in the
/// host. Cancelling is not a failure.
///
/// A `Window`, not a `WebviewWindow`: with the side panel open the window
/// holds a second webview, and a `WebviewWindow` then fails to extract.
#[tauri::command]
pub async fn extensions_choose_folder<R: Runtime>(
    window: Window<R>,
    host: State<'_, Arc<ExtHost>>,
    picker: State<'_, Picker<R>>,
) -> Result<Value, String> {
    if let Some(refusal) = needs_node(&host).await {
        return Ok(refusal);
    }
    match (picker.0)(window).await {
        Some(path) => host.call("inspect", json!({ "path": path })).await,
        None => Ok(json!({ "ok": false, "cancelled": true })),
    }
}

/// `agentrq:extensions:install-local`
#[tauri::command]
pub async fn extensions_install_local(
    host: State<'_, Arc<ExtHost>>,
    path: Option<String>,
    grant: Option<Object>,
    config: Option<Object>,
) -> Result<Value, String> {
    if let Some(refusal) = needs_node(&host).await {
        return Ok(refusal);
    }
    let params = json!({ "path": path, "grant": grant, "config": config });
    host.call("installLocal", params).await
}

/// `agentrq:extensions:install-catalogue`: only the name crosses; the host
/// looks the entry up in its own index.
#[tauri::command]
pub async fn extensions_install_catalogue(
    host: State<'_, Arc<ExtHost>>,
    full_name: Option<String>,
    grant: Option<Object>,
    config: Option<Object>,
) -> Result<Value, String> {
    if let Some(refusal) = needs_node(&host).await {
        return Ok(refusal);
    }
    let params = json!({ "fullName": full_name, "grant": grant, "config": config });
    host.call("installFromCatalogue", params).await
}

/// `agentrq:extensions:uninstall`
#[tauri::command]
pub async fn extensions_uninstall(
    host: State<'_, Arc<ExtHost>>,
    name: String,
) -> Result<Value, String> {
    host.call("uninstall", json!({ "name": name })).await
}

/// `agentrq:extensions:set-enabled`: turned on by the person, its crashes
/// are forgiven.
#[tauri::command]
pub async fn extensions_set_enabled(
    host: State<'_, Arc<ExtHost>>,
    name: String,
    enabled: bool,
) -> Result<Value, String> {
    if enabled {
        host.forgive(&name);
    }
    let params = json!({ "name": name, "enabled": enabled });
    host.call("setEnabled", params).await
}

/// `agentrq:extensions:configure`
#[tauri::command]
pub async fn extensions_configure(
    host: State<'_, Arc<ExtHost>>,
    name: String,
    values: Option<Object>,
) -> Result<Value, String> {
    let params = json!({ "name": name, "values": values });
    host.call("configure", params).await
}

/// `agentrq:extensions:set-hook-consent`
#[tauri::command]
pub async fn extensions_set_hook_consent(
    host: State<'_, Arc<ExtHost>>,
    name: String,
    level: String,
) -> Result<Value, String> {
    let params = json!({ "name": name, "level": level });
    host.call("setHookConsent", params).await
}

/// `agentrq:extensions:supervisor`
#[tauri::command]
pub async fn extensions_supervisor(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    host.call("supervisor", json!({})).await
}

/// `agentrq:extensions:authorize`
#[tauri::command]
pub async fn extensions_authorize(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    host.call("authorize", json!({})).await
}

/// `agentrq:extensions:deauthorize`
#[tauri::command]
pub async fn extensions_deauthorize(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    host.call("deauthorize", json!({})).await
}

/// `agentrq:extensions:entries`: asked on every render, so a host that cannot
/// answer is nothing contributed.
#[tauri::command]
pub async fn extensions_entries(
    host: State<'_, Arc<ExtHost>>,
    surface: Option<String>,
    context: Option<Object>,
) -> Result<Value, String> {
    let params = json!({ "surface": surface, "context": context });
    Ok(list_or_none(host.call("entries", params).await))
}

/// `agentrq:extensions:drawer`: whether anything draws a format. A format,
/// never a path, crosses the bridge.
#[tauri::command]
pub async fn extensions_drawer(
    host: State<'_, Arc<ExtHost>>,
    format: Option<String>,
) -> Result<Value, String> {
    let answer = host.call("hasDrawer", json!({ "format": format })).await;
    Ok(answer_or_refusal(answer))
}

/// `agentrq:extensions:invoke`
#[tauri::command]
pub async fn extensions_invoke(
    host: State<'_, Arc<ExtHost>>,
    target: Option<Object>,
    context: Option<Object>,
) -> Result<Value, String> {
    let params = json!({ "target": target, "context": context });
    Ok(answer_or_refusal(host.call("invoke", params).await))
}

/// `agentrq:side-panel:pages`: the panel's Pages menu.
#[tauri::command]
pub async fn side_panel_pages(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    Ok(list_or_none(host.call("panelPages", json!({})).await))
}

fn list_or_none(answer: Result<Value, String>) -> Value {
    answer.unwrap_or_else(|_| json!([]))
}

fn answer_or_refusal(answer: Result<Value, String>) -> Value {
    answer.unwrap_or_else(|reason| refused(&reason))
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::ext_host::tests::{script_launch, FakeServices};
    use crate::ext_host::Launch;
    use crate::network::tests::network;
    use crate::test_server::{closed_port, reply, send, Step, TestServer};
    use std::sync::Mutex;
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{
        get_ipc_response, mock_builder, mock_context, noop_assets, MockRuntime, INVOKE_KEY,
    };
    use tauri::webview::InvokeRequest;
    use tauri::Manager;

    #[cfg(windows)]
    const LOCAL: &str = "http://tauri.localhost";
    #[cfg(not(windows))]
    const LOCAL: &str = "tauri://localhost";

    struct Rig {
        services: ShellServices,
        told: Arc<Mutex<Vec<Value>>>,
        _dir: tempfile::TempDir,
    }

    fn services(server_url: &str) -> Rig {
        let dir = tempfile::tempdir().unwrap();
        let key_path = dir.path().join("secrets.key");
        services_keyed(server_url, dir, key_path)
    }

    /// Services whose secrets' key lives at `key_path`.
    fn services_keyed(server_url: &str, dir: tempfile::TempDir, key_path: PathBuf) -> Rig {
        let told = Arc::new(Mutex::new(Vec::new()));
        let log = told.clone();
        let services = ShellServices::new(
            Arc::new(network(dir.path(), server_url)),
            key_path,
            Box::new(move |method, detail| log.lock().unwrap().push(json!([method, detail]))),
            Box::new(|url, redirect| Box::pin(async move { Ok(json!([url, redirect])) })),
        );
        Rig {
            services,
            told,
            _dir: dir,
        }
    }

    async fn ask(rig: &Rig, method: &str, params: Value) -> Result<Value, String> {
        rig.services.serve(method, params).expect(method).await
    }

    #[tokio::test]
    async fn seals_and_opens_with_a_key_kept_in_a_private_file() {
        let rig = services("");
        assert_eq!(
            ask(&rig, "vault", json!({})).await,
            Ok(json!({"available": true}))
        );
        let sealed = ask(&rig, "seal", json!({"texts": ["hunter2", ""]}))
            .await
            .unwrap();
        assert_eq!(sealed["ok"], true);
        let blobs = sealed["sealed"].as_array().unwrap().clone();
        assert!(blobs
            .iter()
            .all(|b| b.as_str().unwrap().starts_with(SEALED_PREFIX)));
        assert!(!blobs[0].as_str().unwrap().contains("hunter2"));

        let mut asked = blobs.clone();
        asked.extend([
            json!("plain"),
            json!("arqv1:!!"),
            json!("arqv1:AAAA"),
            json!(5),
        ]);
        let opened = ask(&rig, "open", json!({"blobs": asked})).await.unwrap();
        assert_eq!(
            opened,
            json!({"opened": ["hunter2", "", null, null, null, null]})
        );

        assert!(rig._dir.path().join("secrets.key").exists());
        let key = rig._dir.path().join("secrets.key");
        let next_launch = services_keyed("", tempfile::tempdir().unwrap(), key);
        let opened = ask(&next_launch, "open", json!({"blobs": [blobs[0]]})).await;
        assert_eq!(opened, Ok(json!({"opened": ["hunter2"]})));
    }

    #[tokio::test]
    async fn refuses_a_secret_until_its_key_can_be_written() {
        let dir = tempfile::tempdir().unwrap();
        let blocker = dir.path().join("blocker");
        std::fs::write(&blocker, b"x").unwrap();
        let rig = services_keyed("", dir, blocker.join("secrets.key"));
        assert_eq!(
            ask(&rig, "vault", json!({})).await,
            Ok(json!({"available": false}))
        );
        assert_eq!(
            ask(&rig, "seal", json!({"texts": ["x"]})).await,
            Ok(json!({"ok": false, "reason": "This system has no secure storage available."}))
        );
        let opened = ask(&rig, "open", json!({"blobs": ["arqv1:AAAA"]})).await;
        assert_eq!(opened, Ok(json!({"opened": [null]})));

        // Asked again, once the folder can be made.
        std::fs::remove_file(&blocker).unwrap();
        let sealed = ask(&rig, "seal", json!({"texts": ["x"]})).await.unwrap();
        assert_eq!(sealed["ok"], true);
    }

    #[tokio::test]
    async fn asks_at_the_same_time_share_one_key() {
        let rig = services("");
        let texts = json!({"texts": ["s"]});
        let (a, b) = tokio::join!(ask(&rig, "seal", texts.clone()), ask(&rig, "seal", texts));
        let blobs = [
            a.unwrap()["sealed"][0].clone(),
            b.unwrap()["sealed"][0].clone(),
        ];
        let opened = ask(&rig, "open", json!({ "blobs": blobs })).await;
        assert_eq!(opened, Ok(json!({"opened": ["s", "s"]})));
    }

    #[tokio::test]
    async fn fetches_through_the_profile_on_its_own_server_only() {
        let server = TestServer::start(|seen| {
            let text = format!(
                "{} {} {}",
                seen.method,
                seen.target,
                String::from_utf8_lossy(&seen.body)
            );
            reply(
                "200 OK",
                &["content-type: text/plain", "connection: close", "x-up: yes"],
                &text,
            )
        })
        .await;
        let rig = services(&server.url);
        let posted = ask(
            &rig,
            "profileFetch",
            json!({
                "url": format!("{}/api/v1/x?y=1", server.url),
                "method": "POST",
                "headers": [["content-type", "application/json"], ["host", "evil"], [1, 2]],
                "body": STANDARD.encode("{}"),
            }),
        )
        .await
        .unwrap();
        assert_eq!(posted["ok"], true);
        assert_eq!(posted["status"], 200);
        assert_eq!(
            STANDARD.decode(posted["body"].as_str().unwrap()).unwrap(),
            b"POST /api/v1/x?y=1 {}"
        );
        let headers = posted["headers"].as_array().unwrap();
        assert!(headers.contains(&json!(["x-up", "yes"])), "{headers:?}");
        assert!(!headers.iter().any(|h| h[0] == "connection"));
        let seen = server.requests();
        assert_eq!(seen[0].header("content-type"), Some("application/json"));

        // A GET sends no body, whatever it was handed.
        let got = ask(
            &rig,
            "profileFetch",
            json!({"url": format!("{}/api/v1/y", server.url), "body": STANDARD.encode("x")}),
        )
        .await
        .unwrap();
        assert_eq!(
            STANDARD.decode(got["body"].as_str().unwrap()).unwrap(),
            b"GET /api/v1/y "
        );

        for (params, reason) in [
            (
                json!({"url": "https://elsewhere.example/x"}),
                "Extensions reach only this profile's AgentRQ server.",
            ),
            (
                json!({}),
                "Extensions reach only this profile's AgentRQ server.",
            ),
            (
                json!({"url": server.url, "method": "NOT A METHOD"}),
                "That is not an HTTP method.",
            ),
            (
                json!({"url": server.url, "method": "POST", "body": "%%"}),
                "The request body was not base64.",
            ),
        ] {
            assert_eq!(
                ask(&rig, "profileFetch", params).await,
                Ok(json!({"ok": false, "reason": reason}))
            );
        }
    }

    #[tokio::test]
    async fn says_why_a_fetch_failed() {
        let gone = closed_port().await;
        let rig = services(&gone);
        let failed = ask(&rig, "profileFetch", json!({"url": format!("{gone}/x")}))
            .await
            .unwrap();
        assert_eq!(failed["reason"], "Cannot reach the AgentRQ server");

        let cut = TestServer::start(|_| {
            vec![
                send("HTTP/1.1 200 OK\r\ncontent-length: 10\r\nconnection: close\r\n\r\nab"),
                Step::Wait(10),
            ]
        })
        .await;
        let rig = services(&cut.url);
        let broke = ask(
            &rig,
            "profileFetch",
            json!({"url": format!("{}/x", cut.url)}),
        )
        .await
        .unwrap();
        assert_eq!(broke["ok"], false);
        assert_eq!(broke["reason"], "Cannot reach the AgentRQ server");

        assert_eq!(reason_for(&ProxyError::Forbidden), "Forbidden");
    }

    #[tokio::test]
    async fn serves_only_its_own_methods_and_passes_on_the_news() {
        let rig = services("http://127.0.0.1:9");
        let asked = json!({"url": "https://a.example/authorize", "redirectUri": "agentrq://x"});
        assert_eq!(
            ask(&rig, "authWindow", asked).await,
            Ok(json!(["https://a.example/authorize", "agentrq://x"]))
        );
        assert_eq!(
            ask(&rig, "authWindow", json!({})).await,
            Ok(json!(["", ""]))
        );
        assert!(rig.services.serve("state", json!({})).is_none());
        assert_eq!(rig.services.server_url(), "http://127.0.0.1:9");
        rig.services.changed("digest", "It crashed three times.");
        rig.services
            .told("panelShow", &json!({"url": "https://a.example/"}));
        assert_eq!(
            *rig.told.lock().unwrap(),
            [
                json!(["changed", {"name": "digest", "reason": "It crashed three times."}]),
                json!(["panelShow", {"url": "https://a.example/"}])
            ]
        );
        let real = ShellServices::new(
            Arc::new(network(rig._dir.path(), "")),
            rig._dir.path().join("secrets.key"),
            untold(),
            Box::new(|_, _| Box::pin(async { Ok(Value::Null) })),
        );
        let asked = real.serve("authWindow", json!({})).expect("served").await;
        assert_eq!(asked, Ok(Value::Null));
        real.told("panelShow", &json!({}));
    }

    /// Where a test sends what the host tells: nowhere.
    pub(crate) fn untold() -> Told {
        Box::new(|_, _| {})
    }

    // the commands

    /// A host that answers every request with what it was asked, and fails
    /// one that says `fail`.
    const ECHO: &str = r#"require('readline').createInterface({ input: process.stdin }).on('line', (line) => {
  const m = JSON.parse(line)
  if (m.id === undefined) return
  const fail = JSON.stringify(m.params).includes('fail')
  const answer = fail ? { error: { code: -32000, message: 'failed' } } : { result: { method: m.method, params: m.params } }
  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: m.id, ...answer }) + '\n')
})"#;

    struct App {
        _app: tauri::App<MockRuntime>,
        webview: tauri::WebviewWindow<MockRuntime>,
        _dir: tempfile::TempDir,
    }

    fn app(launch: Option<Launch>, picked: Option<&'static str>) -> App {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(
            dir.path().join("extensions-installed.json"),
            r#"{"installations":[{"name":"x","enabled":true}]}"#,
        )
        .unwrap();
        let mut builder = mock_builder().invoke_handler(crate::commands::handler());
        if let Some(launch) = launch {
            let host = ExtHost::new(launch, dir.path().into(), Arc::new(FakeServices::default()));
            let picker: FolderPicker<MockRuntime> =
                Box::new(move |_| Box::pin(async move { picked.map(str::to_string) }));
            builder = builder.manage(host).manage(Picker(picker));
        }
        let app = builder
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .expect("mock window");
        App {
            _app: app,
            webview,
            _dir: dir,
        }
    }

    fn call(app: &App, cmd: &str, args: Value) -> Result<Value, Value> {
        call_on(&app.webview, cmd, args)
    }

    /// [`call`], from any thread.
    fn call_on(
        webview: &tauri::WebviewWindow<MockRuntime>,
        cmd: &str,
        args: Value,
    ) -> Result<Value, Value> {
        get_ipc_response(
            webview,
            InvokeRequest {
                cmd: cmd.into(),
                callback: CallbackFn(0),
                error: CallbackFn(1),
                url: LOCAL.parse().unwrap(),
                body: InvokeBody::Json(args),
                headers: Default::default(),
                invoke_key: INVOKE_KEY.to_string(),
            },
        )
        .map(|body| body.deserialize().unwrap())
    }

    const COMMANDS: [&str; 18] = [
        "extensions_state",
        "extensions_download_node",
        "extensions_cancel_node_download",
        "extensions_refresh",
        "extensions_choose_folder",
        "extensions_install_local",
        "extensions_install_catalogue",
        "extensions_uninstall",
        "extensions_set_enabled",
        "extensions_configure",
        "extensions_set_hook_consent",
        "extensions_supervisor",
        "extensions_authorize",
        "extensions_deauthorize",
        "extensions_entries",
        "extensions_drawer",
        "extensions_invoke",
        "side_panel_pages",
    ];

    #[test]
    fn asks_the_host_what_electron_s_handler_did() {
        let app = app(Some(script_launch(ECHO)), Some("/picked"));
        let asked = |cmd: &str, args: Value| {
            let answer = call(&app, cmd, args).unwrap();
            (
                answer["method"].as_str().unwrap_or_default().to_string(),
                answer["params"].clone(),
            )
        };
        assert_eq!(
            asked("extensions_state", json!({})),
            ("state".into(), json!({}))
        );
        assert_eq!(
            asked("extensions_refresh", json!({})),
            ("refresh".into(), json!({}))
        );
        assert_eq!(
            asked("extensions_choose_folder", json!({})),
            ("inspect".into(), json!({"path": "/picked"}))
        );
        assert_eq!(
            asked(
                "extensions_install_local",
                json!({"path": "/p", "grant": {"scope": "workspace"}})
            ),
            (
                "installLocal".into(),
                json!({"path": "/p", "grant": {"scope": "workspace"}, "config": null})
            )
        );
        assert_eq!(
            asked(
                "extensions_install_catalogue",
                json!({"fullName": "a/b", "config": {"k": 1}})
            ),
            (
                "installFromCatalogue".into(),
                json!({"fullName": "a/b", "grant": null, "config": {"k": 1}})
            )
        );
        assert_eq!(
            asked("extensions_uninstall", json!({"name": "x"})),
            ("uninstall".into(), json!({"name": "x"}))
        );
        for enabled in [true, false] {
            assert_eq!(
                asked(
                    "extensions_set_enabled",
                    json!({"name": "x", "enabled": enabled})
                ),
                (
                    "setEnabled".into(),
                    json!({"name": "x", "enabled": enabled})
                )
            );
        }
        assert_eq!(
            asked(
                "extensions_configure",
                json!({"name": "x", "values": {"a": "b"}})
            ),
            (
                "configure".into(),
                json!({"name": "x", "values": {"a": "b"}})
            )
        );
        assert_eq!(
            asked(
                "extensions_set_hook_consent",
                json!({"name": "x", "level": "deny"})
            ),
            (
                "setHookConsent".into(),
                json!({"name": "x", "level": "deny"})
            )
        );
        assert_eq!(
            asked("extensions_supervisor", json!({})),
            ("supervisor".into(), json!({}))
        );
        assert_eq!(
            asked("extensions_authorize", json!({})),
            ("authorize".into(), json!({}))
        );
        assert_eq!(
            asked("extensions_deauthorize", json!({})),
            ("deauthorize".into(), json!({}))
        );
        assert_eq!(
            asked(
                "extensions_entries",
                json!({"surface": "page", "context": {}})
            ),
            ("entries".into(), json!({"surface": "page", "context": {}}))
        );
        assert_eq!(
            asked("extensions_drawer", json!({"format": "mermaid"})),
            ("hasDrawer".into(), json!({"format": "mermaid"}))
        );
        assert_eq!(
            asked(
                "extensions_invoke",
                json!({"target": {"owner": "x"}, "context": {}})
            ),
            (
                "invoke".into(),
                json!({"target": {"owner": "x"}, "context": {}})
            )
        );
        assert_eq!(
            asked("side_panel_pages", json!({})),
            ("panelPages".into(), json!({}))
        );

        // What fails: a page's lists are empty, the rest say why.
        assert_eq!(
            call(&app, "extensions_entries", json!({"surface": "fail"})),
            Ok(json!([]))
        );
        assert_eq!(
            call(&app, "extensions_drawer", json!({"format": "fail"})),
            Ok(json!({"ok": false, "reason": "failed"}))
        );
        assert_eq!(
            call(&app, "extensions_invoke", json!({"target": {"fail": 1}})),
            Ok(json!({"ok": false, "reason": "failed"}))
        );
        assert_eq!(
            call(&app, "extensions_state", json!({})).map(|a| a["method"].clone()),
            Ok(json!("state"))
        );
    }

    #[test]
    fn a_dismissed_folder_dialog_is_cancelled_not_failed() {
        let app = app(Some(script_launch(ECHO)), None);
        assert_eq!(
            call(&app, "extensions_choose_folder", json!({})),
            Ok(json!({"ok": false, "cancelled": true}))
        );
    }

    #[test]
    fn the_folder_dialog_opens_with_the_side_panel_open() {
        let app = app(Some(script_launch(ECHO)), None);
        app.webview
            .as_ref()
            .window()
            .add_child(
                tauri::webview::WebviewBuilder::new("panel", Default::default()),
                tauri::LogicalPosition::new(0, 0),
                tauri::LogicalSize::new(10, 10),
            )
            .expect("mock panel");
        assert_eq!(
            call(&app, "extensions_choose_folder", json!({})),
            Ok(json!({"ok": false, "cancelled": true}))
        );
    }

    #[test]
    fn a_host_that_cannot_answer_lists_nothing_and_says_why_elsewhere() {
        let app = app(Some(script_launch("process.exit(3)")), None);
        assert_eq!(call(&app, "side_panel_pages", json!({})), Ok(json!([])));
        assert!(call(&app, "extensions_state", json!({})).is_err());
    }

    #[test]
    fn refuses_bad_arguments_and_needs_its_state() {
        let app = app(Some(script_launch(ECHO)), None);
        for cmd in [
            "extensions_uninstall",
            "extensions_set_enabled",
            "extensions_configure",
            "extensions_set_hook_consent",
        ] {
            assert!(call(&app, cmd, json!({"name": 5})).is_err(), "{cmd}");
        }
        for cmd in [
            "extensions_install_local",
            "extensions_install_catalogue",
            "extensions_entries",
            "extensions_drawer",
        ] {
            let bad = json!({"path": 5, "fullName": 5, "surface": 5, "format": 5});
            assert!(call(&app, cmd, bad).is_err(), "{cmd}");
        }
        // What comes after a good first argument.
        for (cmd, args) in [
            (
                "extensions_set_enabled",
                json!({"name": "x", "enabled": "yes"}),
            ),
            ("extensions_configure", json!({"name": "x", "values": 5})),
            (
                "extensions_set_hook_consent",
                json!({"name": "x", "level": 5}),
            ),
            (
                "extensions_install_local",
                json!({"path": "/p", "grant": 5}),
            ),
            (
                "extensions_install_local",
                json!({"path": "/p", "config": 5}),
            ),
            (
                "extensions_install_catalogue",
                json!({"fullName": "a/b", "grant": 5}),
            ),
            (
                "extensions_install_catalogue",
                json!({"fullName": "a/b", "config": 5}),
            ),
            (
                "extensions_entries",
                json!({"surface": "page", "context": 5}),
            ),
            ("extensions_invoke", json!({"target": 5})),
            ("extensions_invoke", json!({"target": {}, "context": 5})),
        ] {
            assert!(call(&app, cmd, args.clone()).is_err(), "{cmd} {args}");
        }
        let unpicked = {
            let dir = tempfile::tempdir().unwrap();
            let host = ExtHost::new(
                script_launch(ECHO),
                dir.path().into(),
                Arc::new(FakeServices::default()),
            );
            let app = mock_builder()
                .invoke_handler(crate::commands::handler())
                .manage(host)
                .build(mock_context(noop_assets()))
                .expect("mock app");
            let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
                .build()
                .expect("mock window");
            App {
                _app: app,
                webview,
                _dir: dir,
            }
        };
        assert!(call(&unpicked, "extensions_choose_folder", json!({})).is_err());
        let bare = self::app(None, None);
        for cmd in COMMANDS {
            let err = call(
                &bare,
                cmd,
                json!({"name": "x", "enabled": true, "level": "x"}),
            )
            .expect_err(cmd);
            assert!(
                err.as_str().unwrap_or_default().contains("state"),
                "{cmd}: {err}"
            );
        }
    }

    #[tokio::test]
    async fn draws_from_the_host_on_the_active_origin() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(
            dir.path().join("extensions-installed.json"),
            r#"{"installations":[{"name":"x"}]}"#,
        )
        .unwrap();
        let drawers = HostDrawers {
            host: ExtHost::new(
                script_launch(ECHO),
                dir.path().into(),
                Arc::new(FakeServices::default()),
            ),
            origin: Box::new(|| "app://localhost".into()),
        };
        assert_eq!(drawers.origin(), "app://localhost");
        // The echo is no drawer, and a failure says why.
        assert_eq!(
            drawers.code(Some("mermaid".into())).await,
            DrawerLookup::Refused { reason: None }
        );
        assert_eq!(
            drawers.code(Some("fail".into())).await,
            DrawerLookup::Refused {
                reason: Some("failed".into())
            }
        );
    }

    /// An app whose host is checked against a Node from `url`, and run as
    /// the echo, with one extension installed and a catalogue cached.
    fn on_node(url: &str) -> (App, Arc<crate::ext_node::Node>) {
        use crate::ext_node::tests::{node_from, pin_of, BINARY};
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(
            dir.path().join("extensions-installed.json"),
            r#"{"installations":[{"name":"x","version":"1.0.0"}]}"#,
        )
        .unwrap();
        std::fs::write(
            dir.path().join("extensions-catalogue.json"),
            r#"{"entries":[{"ok":true,"fullName":"a/b"}],"fetchedAt":1}"#,
        )
        .unwrap();
        let node = node_from(dir.path(), url, pin_of(BINARY));
        let host = ExtHost::checked_by(
            script_launch(ECHO),
            node.clone(),
            dir.path().into(),
            Arc::new(FakeServices::default()),
        );
        let picker: FolderPicker<MockRuntime> = Box::new(|_| Box::pin(async { None }));
        let app = mock_builder()
            .invoke_handler(crate::commands::handler())
            .manage(host)
            .manage(Picker(picker))
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .expect("mock window");
        let app = App {
            _app: app,
            webview,
            _dir: dir,
        };
        (app, node)
    }

    #[test]
    fn without_node_the_page_reads_from_disk_and_nothing_downloads_on_the_way() {
        let (app, node) = on_node("");
        let state = call(&app, "extensions_state", json!({})).unwrap();
        assert_eq!(state["index"]["entries"][0]["fullName"], "a/b");
        assert_eq!(state["installed"][0]["name"], "x");
        assert_eq!(state["installed"][0]["loaded"], false);
        assert_eq!(state["node"]["installed"], false);
        assert_eq!(state["node"]["update"], false);
        assert_eq!(
            state["node"]["version"],
            agentrq_desktop_core::ext_node::NODE_VERSION
        );

        let refused = json!({
            "ok": false,
            "needsNode": true,
            "reason": needs_node_reason(),
        });
        for (cmd, args) in [
            ("extensions_refresh", json!({})),
            ("extensions_choose_folder", json!({})),
            ("extensions_install_local", json!({"path": "/p"})),
            ("extensions_install_catalogue", json!({"fullName": "a/b"})),
        ] {
            assert_eq!(call(&app, cmd, args), Ok(refused.clone()), "{cmd}");
        }
        // Nothing was fetched, and the host never started.
        assert!(!node.binary().exists());
        assert!(!app.webview.state::<Arc<ExtHost>>().is_running());
        // A call that needs the host says why it cannot have it.
        assert_eq!(
            call(&app, "extensions_uninstall", json!({"name": "x"})),
            Err(json!(needs_node_reason()))
        );
    }

    #[test]
    fn downloads_node_only_when_asked_then_starts_what_is_enabled() {
        use crate::ext_node::tests::{archive_of, serving, BINARY};
        let server = tauri::async_runtime::block_on(serving(archive_of(BINARY)));
        let (app, node) = on_node(&server.url);
        let heard = crate::side_panel::tests::Heard::new(app.webview.app_handle(), NODE_PROGRESS);
        assert_eq!(
            call(&app, "extensions_download_node", json!({})),
            Ok(json!({"ok": true}))
        );
        assert_eq!(
            heard.all().last().map(|detail| &detail["percent"]),
            Some(&json!(100))
        );
        assert_eq!(node.check(), crate::ext_node::Health::Ready(node.binary()));
        // The installed extension is wanted, so its host started.
        assert!(app.webview.state::<Arc<ExtHost>>().is_running());

        let state = call(&app, "extensions_state", json!({})).unwrap();
        assert_eq!(state["method"], "state");
        assert_eq!(state["node"]["installed"], true);
        assert_eq!(
            call(&app, "extensions_refresh", json!({})).unwrap()["method"],
            "refresh"
        );
        assert_eq!(
            call(&app, "extensions_choose_folder", json!({})),
            Ok(json!({"ok": false, "cancelled": true}))
        );
        assert_eq!(server.requests().len(), 1);
        let host = app.webview.state::<Arc<ExtHost>>().inner().clone();
        tauri::async_runtime::block_on(async move { host.stop() });
    }

    #[test]
    fn a_download_says_why_it_failed_or_that_it_was_cancelled() {
        let (app, _) = on_node(&closed_port_blocking());
        let failed = call(&app, "extensions_download_node", json!({})).unwrap();
        assert_eq!(failed["ok"], false);
        assert!(
            failed["reason"]
                .as_str()
                .unwrap()
                .starts_with("The Node.js download failed: "),
            "{failed}"
        );

        // The pinned length, then ten bytes, then nothing.
        let length = crate::ext_node::tests::archive_of(crate::ext_node::tests::BINARY).len();
        let server = tauri::async_runtime::block_on(TestServer::start(move |_| {
            let head = format!("HTTP/1.1 200 OK\r\nContent-Length: {length}\r\n\r\n");
            vec![send(&head), send("0123456789"), Step::Wait(600_000)]
        }));
        let (app, _) = on_node(&server.url);
        assert_eq!(
            call(&app, "extensions_cancel_node_download", json!({})),
            Ok(json!(false))
        );
        let heard = crate::side_panel::tests::Heard::new(app.webview.app_handle(), NODE_PROGRESS);
        let webview = app.webview.clone();
        let download =
            std::thread::spawn(move || call_on(&webview, "extensions_download_node", json!({})));
        crate::side_panel::tests::wait_for("the download to start", &|| !heard.all().is_empty());
        assert_eq!(
            call(&app, "extensions_cancel_node_download", json!({})),
            Ok(json!(true))
        );
        assert_eq!(
            download.join().unwrap(),
            Ok(json!({"ok": false, "cancelled": true}))
        );
    }

    #[test]
    fn a_host_on_the_path_s_node_needs_no_download() {
        let app = app(Some(script_launch(ECHO)), None);
        assert_eq!(
            call(&app, "extensions_download_node", json!({})),
            Ok(json!({"ok": true}))
        );
        assert_eq!(
            call(&app, "extensions_cancel_node_download", json!({})),
            Ok(json!(false))
        );
    }

    /// An address nothing listens on, from a test with no runtime of its own.
    fn closed_port_blocking() -> String {
        tauri::async_runtime::block_on(closed_port())
    }
}
