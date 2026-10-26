// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! `window.agentrq.extensions`, and what the extension host asks of the shell.
//!
//! Each command is Electron's `agentrq:extensions:*` handler, answered by the
//! extension host (`ext_host.rs`). The host in turn asks the shell for what
//! only the shell has: a fetch through the profile's jar, and the keychain.

use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;

use agentrq_desktop_core::ext_host::{fetch_allowed, unavailable};
use agentrq_desktop_core::protocol::{
    drawer_lookup, filter_request_headers, filter_response_headers, proxy_error_reply, sends_body,
    DrawerLookup, ProxyError,
};
use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use serde_json::{json, Value};
use tauri::{AppHandle, Emitter, Manager, Runtime, State, WebviewWindow};

use crate::ext_host::{Answer, ExtHost, Services};
use crate::network::{Network, KEYCHAIN_DEADLINE};
use crate::scheme::Drawers;
use crate::upstream::{self, pairs, BodyLimits, Outgoing};
use crate::vault::{keychain_only_key, Keychain, Vault};

/// What a value the shell sealed for the host starts with (`vault.js`).
pub const SEALED_PREFIX: &str = "arqv1:";

pub const KEYCHAIN_COOLDOWN: Duration = Duration::from_millis(1000);

/// The extension secrets' keychain, separate from the cookie jars' key.
pub type SecretsKeychain = Arc<dyn Keychain + Send + Sync>;

/// What the host is answered with, and where its news goes.
pub struct ShellServices(Arc<Shell>);

struct VaultCache {
    vault: Option<Vault>,
    failed_at: Option<std::time::Instant>,
}

struct Shell {
    network: Arc<Network>,
    keychain: SecretsKeychain,
    keychain_deadline: Duration,
    keychain_cooldown: Duration,
    vault: tokio::sync::Mutex<VaultCache>,
    on_changed: Box<dyn Fn(Value) + Send + Sync>,
}

impl ShellServices {
    pub fn new(
        network: Arc<Network>,
        keychain: SecretsKeychain,
        on_changed: Box<dyn Fn(Value) + Send + Sync>,
    ) -> Self {
        Self::tuned(
            network,
            keychain,
            on_changed,
            KEYCHAIN_DEADLINE,
            KEYCHAIN_COOLDOWN,
        )
    }

    fn tuned(
        network: Arc<Network>,
        keychain: SecretsKeychain,
        on_changed: Box<dyn Fn(Value) + Send + Sync>,
        keychain_deadline: Duration,
        keychain_cooldown: Duration,
    ) -> Self {
        Self(Arc::new(Shell {
            network,
            keychain,
            keychain_deadline,
            keychain_cooldown,
            vault: tokio::sync::Mutex::new(VaultCache {
                vault: None,
                failed_at: None,
            }),
            on_changed,
        }))
    }
}

impl Shell {
    /// The secrets' vault, cached when available; re-queries after cooldown
    /// when the last attempt failed.
    async fn vault(&self) -> Option<Vault> {
        let mut cache = self.vault.lock().await;
        if let Some(vault) = &cache.vault {
            return Some(vault.clone());
        }
        if let Some(failed_at) = cache.failed_at {
            if failed_at.elapsed() < self.keychain_cooldown {
                return None;
            }
        }
        let keychain = self.keychain.clone();
        let read = tokio::task::spawn_blocking(move || keychain_only_key(&*keychain));
        match tokio::time::timeout(self.keychain_deadline, read).await {
            Ok(Ok(Some(key))) => {
                let vault = Vault::new(&key);
                cache.vault = Some(vault.clone());
                cache.failed_at = None;
                Some(vault)
            }
            _ => {
                cache.failed_at = Some(std::time::Instant::now());
                None
            }
        }
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
            limits: BodyLimits::default(),
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
            // WP9 part 2: the supervisor's OAuth window.
            "authWindow" => {
                Box::pin(async { Err(unavailable("The authorisation window", "WP9 part 2")) })
            }
            _ => return None,
        })
    }

    fn changed(&self, name: &str, reason: &str) {
        (self.0.on_changed)(json!({ "name": name, "reason": reason }));
    }

    fn server_url(&self) -> String {
        self.0.network.server_url()
    }
}

/// The folder `chooseFolder` picked; `None` when the dialog was dismissed.
pub type PickedFolder = Pin<Box<dyn Future<Output = Option<String>> + Send>>;

/// Opens the dialog for `chooseFolder`, over the window that asked.
pub type FolderPicker<R> = Box<dyn Fn(WebviewWindow<R>) -> PickedFolder + Send + Sync>;

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

/// `agentrq:extensions:state`
#[tauri::command]
pub async fn extensions_state(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    if host.is_production_node() && crate::ext_node::verify_node(host.data_dir()).is_err() {
        return Ok(json!({
            "index": { "entries": [] },
            "installed": [],
            "nodeInstalled": false,
            "nodeDownloadSize": "about 40 MB",
        }));
    }
    let mut state = host.call("state", json!({})).await?;
    if let Value::Object(ref mut map) = state {
        map.insert("nodeInstalled".to_string(), json!(true));
    }
    Ok(state)
}

/// `agentrq:extensions:refresh`
#[tauri::command]
pub async fn extensions_refresh(host: State<'_, Arc<ExtHost>>) -> Result<Value, String> {
    if host.is_production_node() && crate::ext_node::verify_node(host.data_dir()).is_err() {
        return Ok(json!({ "entries": [] }));
    }
    host.call("refresh", json!({})).await
}

async fn ensure_node_available_internal<R: Runtime>(
    app: &AppHandle<R>,
    host: &ExtHost,
    base_url: Option<&str>,
    pin_override: Option<&agentrq_desktop_core::ext_node::NodePin>,
) -> Result<(), String> {
    let check = match pin_override {
        Some(pin) => crate::ext_node::verify_node_with_pin(host.data_dir(), pin),
        None => crate::ext_node::verify_node(host.data_dir()),
    };
    if host.is_production_node() && check.is_err() {
        let emitter = app.clone();
        let progress_emitter = emitter.clone();
        let on_progress: Arc<dyn Fn(u64, u64) + Send + Sync> =
            Arc::new(move |downloaded, total| {
                let percent = ((downloaded as f64 / (total.max(1)) as f64) * 100.0).round() as u32;
                let _ = progress_emitter.emit(
                    "agentrq:extensions:changed",
                    json!({
                        "reason": "node-download-progress",
                        "downloaded": downloaded,
                        "total": total,
                        "percent": percent,
                    }),
                );
            });
        crate::ext_node::download_and_install_node(
            host.data_dir(),
            None,
            base_url,
            Some(on_progress),
            pin_override,
        )
        .await?;
        let _ = emitter.emit(
            "agentrq:extensions:changed",
            json!({
                "reason": "node-download-complete",
            }),
        );
    }
    Ok(())
}

async fn ensure_node_available<R: Runtime>(
    app: &AppHandle<R>,
    host: &ExtHost,
) -> Result<(), String> {
    ensure_node_available_internal(app, host, None, None).await
}

/// `agentrq:extensions:choose-folder`: the dialog here, the reading in the
/// host. Cancelling is not a failure.
#[tauri::command]
pub async fn extensions_choose_folder<R: Runtime>(
    window: WebviewWindow<R>,
    host: State<'_, Arc<ExtHost>>,
    picker: State<'_, Picker<R>>,
) -> Result<Value, String> {
    match (picker.0)(window.clone()).await {
        Some(path) => {
            ensure_node_available(window.app_handle(), &host).await?;
            host.call("inspect", json!({ "path": path })).await
        }
        None => Ok(json!({ "ok": false, "cancelled": true })),
    }
}

/// `agentrq:extensions:install-local`
#[tauri::command]
pub async fn extensions_install_local<R: Runtime>(
    app: AppHandle<R>,
    host: State<'_, Arc<ExtHost>>,
    path: Option<String>,
    grant: Option<Object>,
    config: Option<Object>,
) -> Result<Value, String> {
    ensure_node_available(&app, &host).await?;
    let Some(path) = path else {
        return Ok(json!({ "ok": true }));
    };
    let params = json!({ "path": path, "grant": grant, "config": config });
    host.call("installLocal", params).await
}

/// `agentrq:extensions:install-catalogue`: only the name crosses; the host
/// looks the entry up in its own index.
#[tauri::command]
pub async fn extensions_install_catalogue<R: Runtime>(
    app: AppHandle<R>,
    host: State<'_, Arc<ExtHost>>,
    full_name: Option<String>,
    grant: Option<Object>,
    config: Option<Object>,
) -> Result<Value, String> {
    ensure_node_available(&app, &host).await?;
    let Some(full_name) = full_name else {
        return Ok(json!({ "ok": true }));
    };
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
mod tests {
    use super::*;
    use crate::ext_host::tests::{script_launch, FakeServices};
    use crate::ext_host::Launch;
    use crate::network::tests::network;
    use crate::test_server::{closed_port, reply, send, Step, TestServer};
    use crate::vault::KeychainError;
    use std::sync::Mutex;
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{
        get_ipc_response, mock_builder, mock_context, noop_assets, MockRuntime, INVOKE_KEY,
    };
    use tauri::webview::InvokeRequest;

    #[cfg(windows)]
    const LOCAL: &str = "http://tauri.localhost";
    #[cfg(not(windows))]
    const LOCAL: &str = "tauri://localhost";

    /// A keychain in memory: holding a key, refusing, or slow to answer.
    #[derive(Default)]
    struct MemKeychain {
        held: Mutex<Option<Vec<u8>>>,
        refuses: Mutex<bool>,
        delay_ms: u64,
    }

    impl Keychain for MemKeychain {
        fn get(&self) -> Result<Option<Vec<u8>>, KeychainError> {
            std::thread::sleep(Duration::from_millis(self.delay_ms));
            if *self.refuses.lock().unwrap() {
                return Err(KeychainError::Refused);
            }
            Ok(self.held.lock().unwrap().clone())
        }
        fn set(&self, secret: &[u8]) -> Result<(), KeychainError> {
            *self.held.lock().unwrap() = Some(secret.to_vec());
            Ok(())
        }
    }

    struct Rig {
        services: ShellServices,
        told: Arc<Mutex<Vec<Value>>>,
        _dir: tempfile::TempDir,
    }

    fn services(server_url: &str, keychain: MemKeychain) -> Rig {
        services_tuned(
            server_url,
            Arc::new(keychain),
            Duration::from_millis(300),
            KEYCHAIN_COOLDOWN,
        )
    }

    fn services_tuned(
        server_url: &str,
        keychain: Arc<dyn Keychain + Send + Sync>,
        keychain_deadline: Duration,
        keychain_cooldown: Duration,
    ) -> Rig {
        let dir = tempfile::tempdir().unwrap();
        let told = Arc::new(Mutex::new(Vec::new()));
        let log = told.clone();
        let services = ShellServices::tuned(
            Arc::new(network(dir.path(), server_url)),
            keychain,
            Box::new(move |detail| log.lock().unwrap().push(detail)),
            keychain_deadline,
            keychain_cooldown,
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
    async fn seals_and_opens_with_a_key_kept_only_in_the_keychain() {
        let rig = services("", MemKeychain::default());
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
    }

    #[tokio::test]
    async fn refuses_a_secret_without_a_keychain_and_opens_nothing() {
        for keychain in [
            MemKeychain {
                refuses: Mutex::new(true),
                ..Default::default()
            },
            MemKeychain {
                delay_ms: 2_000,
                ..Default::default()
            },
        ] {
            let rig = services("", keychain);
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
        }
    }

    #[tokio::test]
    async fn asking_again_after_a_keychain_failure_recovers_once_unlocked() {
        let key = [7u8; 32];
        let keychain = Arc::new(MemKeychain {
            refuses: Mutex::new(true),
            ..Default::default()
        });
        let rig = services_tuned(
            "http://127.0.0.1:9",
            keychain.clone(),
            Duration::from_millis(50),
            Duration::from_millis(50),
        );
        let res = ask(&rig, "seal", json!({"texts": ["secret"]}))
            .await
            .unwrap();
        assert_eq!(res["ok"], false);

        // Before cooldown expires: still fails
        let res_fast = ask(&rig, "seal", json!({"texts": ["secret"]}))
            .await
            .unwrap();
        assert_eq!(res_fast["ok"], false);

        // Wait past cooldown
        tokio::time::sleep(Duration::from_millis(60)).await;

        // Keychain is now unlocked and returns key
        *keychain.refuses.lock().unwrap() = false;
        *keychain.held.lock().unwrap() = Some(key.to_vec());

        // Second attempt asks again and succeeds!
        let res2 = ask(&rig, "seal", json!({"texts": ["secret"]}))
            .await
            .unwrap();
        assert_eq!(res2["ok"], true);
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
        let rig = services(&server.url, MemKeychain::default());
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
        let rig = services(&gone, MemKeychain::default());
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
        let rig = services(&cut.url, MemKeychain::default());
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
        let rig = services("http://127.0.0.1:9", MemKeychain::default());
        assert_eq!(
            ask(&rig, "authWindow", json!({})).await,
            Err("The authorisation window is unavailable in this preview until WP9 part 2.".into())
        );
        assert!(rig.services.serve("state", json!({})).is_none());
        assert_eq!(rig.services.server_url(), "http://127.0.0.1:9");
        rig.services.changed("digest", "It crashed three times.");
        assert_eq!(
            *rig.told.lock().unwrap(),
            [json!({"name": "digest", "reason": "It crashed three times."})]
        );
        let real = ShellServices::new(
            Arc::new(network(rig._dir.path(), "")),
            Arc::new(MemKeychain::default()),
            Box::new(drop),
        );
        assert_eq!(real.0.keychain_deadline, KEYCHAIN_DEADLINE);
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
        get_ipc_response(
            &app.webview,
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

    const COMMANDS: [&str; 16] = [
        "extensions_state",
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

    #[tokio::test]
    async fn extensions_state_and_install_with_production_node() {
        let dir = tempfile::tempdir().unwrap();
        let prod_bin = crate::ext_node::node_binary_path(dir.path());
        let host = ExtHost::new(
            crate::ext_host::Launch {
                program: prod_bin,
                args: vec![],
            },
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
        let app_rig = App {
            _app: app,
            webview,
            _dir: dir,
        };

        // Node is missing: state returns uninstalled cleanly without starting Node
        let state = call(&app_rig, "extensions_state", json!({})).unwrap();
        assert_eq!(state["nodeInstalled"], false);
        assert_eq!(state["nodeDownloadSize"], "about 40 MB");

        // refresh returns empty entries when node missing without crashing
        let refreshed = call(&app_rig, "extensions_refresh", json!({})).unwrap();
        assert_eq!(refreshed["entries"], json!([]));

        // Choose folder cancelled when picker returns None
        let chosen = call(&app_rig, "extensions_choose_folder", json!({})).unwrap();
        assert_eq!(chosen["cancelled"], true);

        // Download mock verified node via ensure_node_available_internal
        let (archive_bytes, archive_hash) =
            crate::ext_node::tests::make_mock_tarball(b"#!/bin/sh\nexit 0\n");
        let server = TestServer::start(move |_seen| {
            let mut head = format!(
                "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                archive_bytes.len()
            )
            .into_bytes();
            head.extend_from_slice(&archive_bytes);
            vec![Step::Send(head)]
        })
        .await;

        let pin = agentrq_desktop_core::ext_node::current_pin().unwrap();
        let mock_pin = agentrq_desktop_core::ext_node::NodePin {
            file: pin.file,
            sha256: Box::leak(archive_hash.into_boxed_str()),
        };

        ensure_node_available_internal(
            app_rig.webview.app_handle(),
            &app_rig.webview.state::<Arc<ExtHost>>(),
            Some(&server.url),
            Some(&mock_pin),
        )
        .await
        .unwrap();

        assert!(crate::ext_node::verify_node_with_pin(app_rig._dir.path(), &mock_pin).is_ok());

        // Second call is noop because node is already verified
        ensure_node_available_internal(
            app_rig.webview.app_handle(),
            &app_rig.webview.state::<Arc<ExtHost>>(),
            Some(&server.url),
            Some(&mock_pin),
        )
        .await
        .unwrap();

        // Update stamp to match current_pin for install_local and install_catalogue
        let bytes = b"#!/bin/sh\nexit 0\n";
        let bin_hash = agentrq_desktop_core::ext_node::compute_sha256(bytes);
        let stamp_content =
            agentrq_desktop_core::ext_node::format_stamp(&bin_hash, bytes.len() as u64, pin.sha256);
        std::fs::write(
            crate::ext_node::stamp_path(app_rig._dir.path()),
            stamp_content,
        )
        .unwrap();

        // Node is verified now: install_local with None succeeds
        let res_local = call(
            &app_rig,
            "extensions_install_local",
            json!({ "path": null }),
        )
        .unwrap();
        assert_eq!(res_local["ok"], true);

        // install_catalogue with None succeeds
        let res_cat = call(
            &app_rig,
            "extensions_install_catalogue",
            json!({ "fullName": null }),
        )
        .unwrap();
        assert_eq!(res_cat["ok"], true);
    }
}
