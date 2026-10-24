// WP0 throwaway spike for the Tauri port (pano 0kt2DmAD8h5). Never merge.
//
// One binary, one check per run: SPIKE picks it, SERVER is the stand-in
// backend (testserver/server.mjs), OUT is where the results JSON goes.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod snapshot;
#[cfg(target_os = "linux")]
mod gtkpanel;

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use futures_util::StreamExt;
use include_dir::{include_dir, Dir};
use reqwest::cookie::Jar;
use serde::Deserialize;
use serde_json::{json, Value};
use tauri::ipc::Channel;
use tauri::webview::{PageLoadEvent, WebviewBuilder};
use tauri::{
    AppHandle, LogicalPosition, LogicalSize, Manager, Runtime, Url, WebviewUrl, WebviewWindowBuilder,
};
use tauri_plugin_notification::NotificationExt;

/// Enough of `window.agentrq` for desktop/src/renderer/main.js to boot to the
/// connection screen: the S9 memory comparison runs both shells on that screen.
const RENDERER_STUB: &str = r#"
(function () {
  const none = () => Promise.resolve(null)
  const sub = () => () => {}
  const deep = (path) => new Proxy(function () {}, {
    get: (_, k) => (k === 'then' ? undefined : (String(k).startsWith('on') ? sub : deep(path + '.' + String(k)))),
    apply: () => Promise.resolve(null),
  })
  const os = navigator.platform.startsWith('Mac') ? 'darwin' : navigator.platform.startsWith('Win') ? 'win32' : 'linux'
  window.agentrq = new Proxy({
    isDesktop: true, platform: os, versions: { tauri: '2', webview: navigator.userAgent },
    connection: { get: () => Promise.resolve({ configured: false, serverUrl: '', locked: false, canCancel: false }),
      validate: none, save: none, cancel: none },
  }, { get: (t, k) => (k in t ? t[k] : deep(String(k))) })
})()
"#;

static UI: Dir = include_dir!("$CARGO_MANIFEST_DIR/../ui");

struct Profile {
    jar: Arc<Jar>,
    client: reqwest::Client,
}

impl Profile {
    fn new() -> Self {
        let jar = Arc::new(Jar::default());
        let client = reqwest::Client::builder()
            .cookie_provider(jar.clone())
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .expect("client");
        Profile { jar, client }
    }
}

struct Spike {
    name: String,
    server: Url,
    out: PathBuf,
    data: PathBuf,
    started: Instant,
    results: Mutex<serde_json::Map<String, Value>>,
    profiles: Mutex<HashMap<String, Arc<Profile>>>,
    sse: Mutex<HashMap<u32, tokio::task::AbortHandle>>,
    next_id: AtomicU32,
}

impl Spike {
    fn report(&self, key: &str, value: Value) {
        eprintln!("[spike] {key} = {value}");
        if key == "s8-targets" {
            let _ = std::fs::write(self.out.parent().unwrap().join("s8-targets.json"), value.to_string());
        }
        self.results.lock().unwrap().insert(key.to_string(), value);
    }

    fn profile(&self, label: &str) -> Arc<Profile> {
        let id = label.rsplit('-').next().unwrap_or(label).to_string();
        self.profiles
            .lock()
            .unwrap()
            .entry(id)
            .or_insert_with(|| Arc::new(Profile::new()))
            .clone()
    }

    fn write(&self, status: &str) {
        let mut r = self.results.lock().unwrap().clone();
        r.insert("_status".into(), json!(status));
        r.insert("_spike".into(), json!(self.name));
        r.insert("_os".into(), json!(std::env::consts::OS));
        r.insert("_arch".into(), json!(std::env::consts::ARCH));
        r.insert("_elapsedMs".into(), json!(self.started.elapsed().as_millis() as u64));
        let _ = std::fs::create_dir_all(self.out.parent().unwrap_or(&self.out));
        let _ = std::fs::write(&self.out, serde_json::to_vec_pretty(&Value::Object(r)).unwrap());
    }
}

/// `app://localhost/x` on macOS and Linux, `http://app.localhost/x` on Windows.
fn app_url(host: &str, path: &str) -> Url {
    if cfg!(windows) {
        Url::parse(&format!("http://app.{host}{path}")).unwrap()
    } else {
        Url::parse(&format!("app://{host}{path}")).unwrap()
    }
}

fn profile_dir(spike: &Spike, id: &str) -> PathBuf {
    spike.data.join(id)
}

/// Gives a webview a profile's own storage: a data dir, or on macOS 14+ a data
/// store identifier, since WKWebView has no data dir.
macro_rules! isolate {
    ($builder:expr, $spike:expr, $id:expr) => {{
        let b = $builder;
        #[cfg(not(target_os = "macos"))]
        let b = b.data_directory(profile_dir($spike, $id));
        #[cfg(target_os = "macos")]
        let b = if std::env::var("S5_HOST").is_ok() {
            b
        } else {
            b.data_store_identifier(store_id($id))
        };
        b
    }};
}

#[cfg(target_os = "macos")]
fn store_id(id: &str) -> [u8; 16] {
    let mut out = [0u8; 16];
    for (i, b) in format!("agentrq-{id}").bytes().enumerate() {
        out[i % 16] ^= b.wrapping_add(i as u8);
    }
    out[6] = (out[6] & 0x0f) | 0x40;
    out[8] = (out[8] & 0x3f) | 0x80;
    out
}

// ---------------------------------------------------------------- the scheme

fn static_file(root: &str, path: &str) -> http::Response<Vec<u8>> {
    let rel = path.trim_start_matches('/');
    let rel = if rel.is_empty() { "index.html" } else { rel };
    let full = if root.is_empty() { rel.to_string() } else { format!("{root}/{rel}") };
    let file = UI.get_file(&full).or_else(|| {
        // SPA fallback, as the Go backend and protocol.js do.
        if !rel.contains('.') {
            UI.get_file(if root.is_empty() { "index.html".to_string() } else { format!("{root}/index.html") })
        } else {
            None
        }
    });
    match file {
        Some(f) => {
            let mime = match f.path().extension().and_then(|e| e.to_str()) {
                Some("html") => "text/html; charset=utf-8",
                Some("js") | Some("mjs") => "text/javascript",
                Some("css") => "text/css",
                Some("svg") => "image/svg+xml",
                Some("png") => "image/png",
                Some("json") | Some("webmanifest") => "application/json",
                Some("woff2") => "font/woff2",
                Some("wasm") => "application/wasm",
                _ => "application/octet-stream",
            };
            http::Response::builder()
                .status(200)
                .header("content-type", mime)
                .header("cache-control", "no-store")
                .body(f.contents().to_vec())
                .unwrap()
        }
        None => http::Response::builder().status(404).body(b"not found".to_vec()).unwrap(),
    }
}

async fn proxy(spike: &Spike, label: &str, req: http::Request<Vec<u8>>) -> http::Response<Vec<u8>> {
    let profile = spike.profile(label);
    let pq = req.uri().path_and_query().map(|p| p.as_str()).unwrap_or("/");
    let target = spike.server.join(pq).unwrap();
    let method = reqwest::Method::from_bytes(req.method().as_str().as_bytes()).unwrap();
    let mut rb = profile.client.request(method, target);
    for (k, v) in req.headers() {
        let k = k.as_str();
        if matches!(k, "origin" | "referer" | "host" | "content-length" | "cookie") {
            continue;
        }
        rb = rb.header(k, v.as_bytes());
    }
    let has_body = !req.body().is_empty();
    let deadline = Duration::from_secs(if has_body { 120 } else { 30 });
    let fut = async {
        let res = rb.body(req.into_body()).send().await?;
        let status = res.status().as_u16();
        let headers = res.headers().clone();
        let body = res.bytes().await?;
        Ok::<_, reqwest::Error>((status, headers, body))
    };
    match tokio::time::timeout(deadline, fut).await {
        Ok(Ok((status, headers, body))) => {
            let mut b = http::Response::builder().status(status);
            for (k, v) in headers.iter() {
                let k = k.as_str();
                if k == "set-cookie" || k == "content-encoding" || k == "content-length" || k.starts_with("access-control-") {
                    continue;
                }
                b = b.header(k, v.as_bytes());
            }
            b.body(body.to_vec()).unwrap()
        }
        Ok(Err(e)) => http::Response::builder().status(502).body(e.to_string().into_bytes()).unwrap(),
        Err(_) => http::Response::builder().status(504).body(b"deadline".to_vec()).unwrap(),
    }
}

async fn handle<R: Runtime>(app: AppHandle<R>, label: String, req: http::Request<Vec<u8>>) -> http::Response<Vec<u8>> {
    let spike = app.state::<Arc<Spike>>().inner().clone();
    let path = req.uri().path().to_string();
    if path.starts_with("/api/") || path == "/mcp" || path.starts_with("/mcp/") || path.starts_with("/.well-known/") {
        return proxy(&spike, &label, req).await;
    }
    if path == "/login" {
        return static_file("", "/login.html");
    }
    if path == "/__spike/echo" {
        let n = req.body().len();
        return http::Response::builder()
            .status(200)
            .header("content-type", "application/json")
            .body(format!("{{\"bytes\":{n}}}").into_bytes())
            .unwrap();
    }
    if path == "/__spike/slow" {
        // A handler that holds its answer: does the page see anything early?
        tokio::time::sleep(Duration::from_millis(1500)).await;
        return http::Response::builder().status(200).body(b"late".to_vec()).unwrap();
    }
    let root = if spike.name == "renderer" { "renderer" } else { "" };
    static_file(root, &path)
}

// ---------------------------------------------------------------- commands

#[derive(Deserialize, Clone, Copy, Debug)]
struct Rect {
    x: f64,
    y: f64,
    w: f64,
    h: f64,
}

#[tauri::command]
fn report(spike: tauri::State<'_, Arc<Spike>>, key: String, value: Value) {
    spike.report(&key, value);
}

#[tauri::command]
fn finish<R: Runtime>(app: AppHandle<R>, spike: tauri::State<'_, Arc<Spike>>) {
    spike.write("done");
    app.exit(0);
}

#[tauri::command]
async fn signin_direct<R: Runtime>(app: AppHandle<R>, webview: tauri::Webview<R>) -> Result<u16, String> {
    let spike = app.state::<Arc<Spike>>().inner().clone();
    let p = spike.profile(webview.label());
    let url = spike.server.join("/api/v1/auth/github/callback?code=x").unwrap();
    let res = p.client.get(url).send().await.map_err(|e| e.to_string())?;
    Ok(res.status().as_u16())
}

#[tauri::command]
async fn jar_clear<R: Runtime>(app: AppHandle<R>, webview: tauri::Webview<R>) {
    let spike = app.state::<Arc<Spike>>().inner().clone();
    let id = webview.label().rsplit('-').next().unwrap().to_string();
    spike.profiles.lock().unwrap().insert(id, Arc::new(Profile::new()));
}

/// The `EventSource` shim's other half: Rust reads the stream through the
/// profile's jar and hands each event to the page over an IPC channel.
#[tauri::command]
async fn sse_open<R: Runtime>(
    app: AppHandle<R>,
    webview: tauri::Webview<R>,
    url: String,
    on_event: Channel<Value>,
) -> Result<u32, String> {
    let spike = app.state::<Arc<Spike>>().inner().clone();
    let p = spike.profile(webview.label());
    let target = spike.server.join(&url).map_err(|e| e.to_string())?;
    let id = spike.next_id.fetch_add(1, Ordering::SeqCst);
    let task = tokio::spawn(async move {
        let res = match p.client.get(target).header("accept", "text/event-stream").send().await {
            Ok(r) => r,
            Err(e) => {
                let _ = on_event.send(json!({"type": "error", "detail": e.to_string()}));
                return;
            }
        };
        if res.status() != 200 {
            let _ = on_event.send(json!({"type": "error", "status": res.status().as_u16()}));
            return;
        }
        let _ = on_event.send(json!({"type": "open"}));
        let mut stream = res.bytes_stream();
        let mut buf = String::new();
        let mut data: Vec<String> = Vec::new();
        let mut event = String::new();
        let mut last_id = String::new();
        loop {
            let next = tokio::time::timeout(Duration::from_secs(75), stream.next()).await;
            let chunk = match next {
                Ok(Some(Ok(c))) => c,
                _ => break,
            };
            buf.push_str(&String::from_utf8_lossy(&chunk));
            while let Some(i) = buf.find('\n') {
                let line = buf[..i].trim_end_matches('\r').to_string();
                buf.drain(..=i);
                if line.is_empty() {
                    if !data.is_empty() {
                        let _ = on_event.send(json!({
                            "type": "message",
                            "event": if event.is_empty() { "message" } else { &event },
                            "data": data.join("\n"),
                            "lastEventId": last_id,
                        }));
                    }
                    data.clear();
                    event.clear();
                } else if let Some(v) = line.strip_prefix("data:") {
                    data.push(v.strip_prefix(' ').unwrap_or(v).to_string());
                } else if let Some(v) = line.strip_prefix("event:") {
                    event = v.trim().to_string();
                } else if let Some(v) = line.strip_prefix("id:") {
                    last_id = v.trim().to_string();
                }
            }
        }
        let _ = on_event.send(json!({"type": "error", "detail": "closed"}));
    });
    spike.sse.lock().unwrap().insert(id, task.abort_handle());
    Ok(id)
}

#[tauri::command]
fn sse_close(spike: tauri::State<'_, Arc<Spike>>, id: u32) {
    if let Some(h) = spike.sse.lock().unwrap().remove(&id) {
        h.abort();
    }
}

#[tauri::command]
async fn set_profile_cookie<R: Runtime>(app: AppHandle<R>, webview: tauri::Webview<R>) -> Result<Value, String> {
    let spike = app.state::<Arc<Spike>>().inner().clone();
    let host = spike.server.host_str().unwrap().to_string();
    let c = tauri::webview::Cookie::build(("at", "tok-123")).domain(host).path("/").http_only(true).build();
    webview.set_cookie(c).map_err(|e| e.to_string())?;
    let back = webview.cookies_for_url(spike.server.clone()).map_err(|e| e.to_string())?;
    Ok(json!({"profileStoreHasAt": back.iter().any(|c| c.name() == "at")}))
}

#[tauri::command]
async fn panel_open<R: Runtime>(app: AppHandle<R>, window: tauri::Window<R>, rect: Rect) -> Result<(), String> {
    let spike = app.state::<Arc<Spike>>().inner().clone();
    let url = spike.server.join("/panel").unwrap();
    let label = format!("panel-{}", window.label());
    let b = WebviewBuilder::new(&label, WebviewUrl::External(url));
    let b = isolate!(b, &spike, &label);
    let panel = window
        .add_child(b, LogicalPosition::new(rect.x, rect.y), LogicalSize::new(rect.w, rect.h))
        .map_err(|e| e.to_string())?;
    #[cfg(target_os = "linux")]
    {
        use gtk::prelude::*;
        let main = app.get_webview(window.label()).ok_or("no main webview")?;
        main.with_webview(|pv| gtkpanel::set_main(pv.inner().upcast()))
            .map_err(|e| e.to_string())?;
        let r = (rect.x as i32, rect.y as i32, rect.w as i32, rect.h as i32);
        panel
            .with_webview(move |pv| {
                let w: gtk::Widget = pv.inner().upcast();
                let vbox = w.parent().and_then(|p| p.downcast::<gtk::Box>().ok()).expect("vbox");
                gtkpanel::adopt(vbox, w, r)
            })
            .map_err(|e| e.to_string())?;
    }
    #[cfg(not(target_os = "linux"))]
    let _ = panel;
    Ok(())
}

fn panel<R: Runtime>(window: &tauri::Window<R>) -> Result<tauri::Webview<R>, String> {
    window
        .app_handle()
        .get_webview(&format!("panel-{}", window.label()))
        .ok_or_else(|| "no panel".to_string())
}

#[tauri::command]
async fn panel_bounds<R: Runtime>(window: tauri::Window<R>, rect: Rect) -> Result<f64, String> {
    let p = panel(&window)?;
    let t = Instant::now();
    #[cfg(target_os = "linux")]
    {
        let r = (rect.x as i32, rect.y as i32, rect.w as i32, rect.h as i32);
        p.with_webview(move |_| gtkpanel::set_rect(r)).map_err(|e| e.to_string())?;
        return Ok(t.elapsed().as_secs_f64() * 1000.0);
    }
    #[allow(unreachable_code)]
    p.set_bounds(tauri::Rect {
        position: LogicalPosition::new(rect.x, rect.y).into(),
        size: LogicalSize::new(rect.w, rect.h).into(),
    })
    .map_err(|e| e.to_string())?;
    Ok(t.elapsed().as_secs_f64() * 1000.0)
}

#[tauri::command]
async fn panel_visible<R: Runtime>(window: tauri::Window<R>, visible: bool) -> Result<f64, String> {
    let p = panel(&window)?;
    let t = Instant::now();
    if visible { p.show() } else { p.hide() }.map_err(|e| e.to_string())?;
    Ok(t.elapsed().as_secs_f64() * 1000.0)
}

#[tauri::command]
async fn panel_info<R: Runtime>(window: tauri::Window<R>) -> Result<Value, String> {
    let p = panel(&window)?;
    let scale = window.scale_factor().map_err(|e| e.to_string())?;
    let pos = p.position().map_err(|e| e.to_string())?;
    let size = p.size().map_err(|e| e.to_string())?;
    Ok(json!({"scale": scale, "x": pos.x, "y": pos.y, "w": size.width, "h": size.height}))
}

/// Mitigation (a): a picture of the guest to show while it is hidden.
#[tauri::command]
async fn panel_snapshot<R: Runtime>(app: AppHandle<R>, window: tauri::Window<R>) -> Result<tauri::ipc::Response, String> {
    let p = panel(&window)?;
    let (tx, rx) = tokio::sync::oneshot::channel();
    let t = Instant::now();
    p.with_webview(move |pv| snapshot::take(pv, tx)).map_err(|e| e.to_string())?;
    let png = tokio::time::timeout(Duration::from_secs(10), rx)
        .await
        .map_err(|_| "snapshot timed out".to_string())?
        .map_err(|_| "snapshot dropped".to_string())??;
    let ms = t.elapsed().as_secs_f64() * 1000.0;
    let spike = app.state::<Arc<Spike>>().inner().clone();
    spike.report("s6-snapshot-native", json!({"ms": ms, "pngBytes": png.len()}));
    Ok(tauri::ipc::Response::new(png))
}

/// Asks the CI screenshot helper (scripts/shotd.mjs) for a screen grab.
#[tauri::command]
async fn shot(spike: tauri::State<'_, Arc<Spike>>, name: String) -> Result<bool, String> {
    let dir = spike.out.parent().unwrap().join("shots");
    let _ = std::fs::create_dir_all(&dir);
    let _ = std::fs::write(dir.join(format!("{name}.req")), b"");
    let png = dir.join(format!("{name}.png"));
    for _ in 0..80 {
        if png.exists() {
            return Ok(true);
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
    Ok(false)
}

#[tauri::command]
fn win_pos<R: Runtime>(window: tauri::Window<R>, spike: tauri::State<'_, Arc<Spike>>) -> Value {
    let s = window.scale_factor().unwrap_or(1.0);
    let p = window.inner_position().map(|p| p.to_logical::<f64>(s)).ok();
    if let Ok(o) = window.outer_position() {
        let l = o.to_logical::<f64>(s);
        let _ = std::fs::write(spike.out.parent().unwrap().join("s8-pos.json"), json!({"x": l.x, "y": l.y}).to_string());
    }
    json!({"scale": s, "x": p.map(|p| p.x), "y": p.map(|p| p.y)})
}

// ---------------------------------------------------------------- spikes

fn main_window<R: Runtime>(app: &AppHandle<R>, spike: &Spike, id: &str, host: &str, page: &str) -> tauri::Result<()> {
    let b = WebviewWindowBuilder::new(app, id, WebviewUrl::CustomProtocol(app_url(host, page)))
        .title(format!("AgentRQ spike {id}"))
        .inner_size(1200.0, 800.0);
    let b = isolate!(b, spike, id);
    let b = if spike.name == "renderer" { b.initialization_script(RENDERER_STUB) } else { b };
    #[cfg(target_os = "macos")]
    let b = if spike.name == "s8" {
        b.title_bar_style(tauri::TitleBarStyle::Overlay)
            .hidden_title(true)
            .traffic_light_position(LogicalPosition::new(16.0, 18.0))
    } else {
        b
    };
    b.build()?;
    Ok(())
}

/// S4: sign in through a webview on the server's origin, lift `at` into the
/// Rust jar, and delete it from the webview.
fn s4<R: Runtime>(app: &AppHandle<R>, spike: Arc<Spike>) -> tauri::Result<()> {
    let start = spike.server.join("/api/v1/auth/github/login").unwrap();
    let done = Arc::new(std::sync::atomic::AtomicBool::new(false));
    let sp = spike.clone();
    let b = WebviewWindowBuilder::new(app, "signin-p1", WebviewUrl::External(start))
        .title("Sign in")
        .on_page_load(move |ww, payload| {
            if payload.event() != PageLoadEvent::Finished {
                return;
            }
            let url = payload.url().clone();
            eprintln!("[spike] signin page loaded {url}");
            if url.path() != "/" || done.swap(true, Ordering::SeqCst) {
                return;
            }
            let spike = sp.clone();
            tauri::async_runtime::spawn(async move {
                let r = s4_check(&ww, &spike).await;
                spike.report("s4", r);
                spike.write("done");
                ww.app_handle().exit(0);
            });
        });
    let b = isolate!(b, &spike, "p1");
    b.build()?;
    Ok(())
}

async fn s4_check<R: Runtime>(ww: &tauri::WebviewWindow<R>, spike: &Spike) -> Value {
    let server = spike.server.clone();
    let mut out = serde_json::Map::new();
    let t = Instant::now();
    let cookies = match ww.cookies_for_url(server.clone()) {
        Ok(c) => c,
        Err(e) => return json!({"pass": false, "error": format!("cookies_for_url: {e}")}),
    };
    out.insert("readMs".into(), json!(t.elapsed().as_secs_f64() * 1000.0));
    out.insert("cookieNames".into(), json!(cookies.iter().map(|c| c.name().to_string()).collect::<Vec<_>>()));
    let Some(at) = cookies.iter().find(|c| c.name() == "at").cloned() else {
        out.insert("pass".into(), json!(false));
        out.insert("error".into(), json!("no at cookie"));
        return Value::Object(out);
    };
    out.insert("httpOnly".into(), json!(at.http_only()));
    out.insert("valueOk".into(), json!(at.value() == "tok-123"));
    out.insert(
        "attrs".into(),
        json!({"domain": at.domain(), "path": at.path(), "secure": at.secure(), "sameSite": at.same_site().map(|s| s.to_string()), "expires": format!("{:?}", at.expires())}),
    );

    // Into the Rust jar, and prove the jar now signs requests in.
    let p = spike.profile("p1");
    p.jar.add_cookie_str(&format!("at={}; Path=/; HttpOnly", at.value()), &server);
    let status = p
        .client
        .get(server.join("/api/v1/auth/user").unwrap())
        .send()
        .await
        .map(|r| r.status().as_u16())
        .unwrap_or(0);
    out.insert("jarUserStatus".into(), json!(status));

    // Out of the webview's own store.
    let t = Instant::now();
    let del = ww.delete_cookie(at.clone());
    out.insert("deleteMs".into(), json!(t.elapsed().as_secs_f64() * 1000.0));
    out.insert("deleteErr".into(), json!(del.err().map(|e| e.to_string())));
    let after = ww.cookies_for_url(server.clone()).map(|c| c.iter().any(|c| c.name() == "at"));
    out.insert("webviewStillHasAt".into(), json!(after.as_ref().ok()));

    // And the server agrees: a fresh request from the webview carries no `at`.
    let _ = ww.navigate(server.join("/report-whoami").unwrap());
    let mut server_saw = Value::Null;
    for _ in 0..50 {
        tokio::time::sleep(Duration::from_millis(100)).await;
        let v: Value = match p.client.get(server.join("/api/v1/__report/s4-after").unwrap()).send().await {
            Ok(r) => r.json().await.unwrap_or(Value::Null),
            Err(_) => Value::Null,
        };
        if !v.is_null() {
            server_saw = v;
            break;
        }
    }
    out.insert("serverAfterDelete".into(), server_saw.clone());
    let pass = at.http_only().unwrap_or(false)
        && status == 200
        && after.as_ref().ok() == Some(&false)
        && server_saw.get("hasAt") == Some(&json!(false));
    out.insert("pass".into(), json!(pass));
    Value::Object(out)
}

fn s5<R: Runtime>(app: &AppHandle<R>, spike: Arc<Spike>, write: bool) -> tauri::Result<()> {
    let host_mode = std::env::var("S5_HOST").is_ok();
    let page = move |id: &str| if write { format!("/s5.html?who={id}") } else { "/s5.html".to_string() };
    let host = move |id: &str| if host_mode { id.to_string() } else { "localhost".to_string() };
    main_window(app, &spike, "p1", &host("p1"), &page("p1"))?;
    let app2 = app.clone();
    std::thread::spawn(move || {
        // The second profile opens once the first has written.
        std::thread::sleep(Duration::from_secs(3));
        let a = app2.clone();
        let _ = app2.run_on_main_thread(move || {
            let spike = a.state::<Arc<Spike>>().inner().clone();
            if let Err(e) = main_window(&a, &spike, "p2", &host("p2"), &page("p2")) {
                spike.report("s5-p2-error", json!(e.to_string()));
            }
        });
    });
    Ok(())
}

fn s6<R: Runtime>(app: &AppHandle<R>, spike: Arc<Spike>) -> tauri::Result<()> {
    let window = tauri::window::WindowBuilder::new(app, "p1")
        .title("AgentRQ spike p1")
        .inner_size(1200.0, 800.0)
        .build()?;
    let b = WebviewBuilder::new("p1", WebviewUrl::CustomProtocol(app_url("localhost", "/s6.html"))).auto_resize();
    let b = isolate!(b, &spike, "p1");
    window.add_child(b, LogicalPosition::new(0.0, 0.0), LogicalSize::new(1200.0, 800.0))?;
    Ok(())
}

fn s7<R: Runtime>(app: &AppHandle<R>, spike: Arc<Spike>) -> tauri::Result<()> {
    main_window(app, &spike, "p1", "localhost", "/blank.html")?;
    let sp = spike.clone();
    let t = Instant::now();
    let a = app.clone();
    app.notification()
        .on_action(move |performed| {
            sp.report(
                "s7-action",
                json!({"performed": serde_json::to_value(performed).ok(), "ms": t.elapsed().as_millis() as u64}),
            );
            sp.write("done");
            a.exit(0);
        })
        .ok();
    let shown = app
        .notification()
        .builder()
        .title("Agent replied")
        .body("spike: click me")
        .show();
    spike.report("s7-show", json!({"ok": shown.is_ok(), "error": shown.err().map(|e| e.to_string())}));
    Ok(())
}

fn main() {
    let name = std::env::var("SPIKE").unwrap_or_else(|_| "s2".into());
    let server = Url::parse(&std::env::var("SERVER").unwrap_or_else(|_| "http://127.0.0.1:3917".into())).unwrap();
    let out = PathBuf::from(std::env::var("OUT").unwrap_or_else(|_| format!("spike-out/{name}.json")));
    let data = PathBuf::from(
        std::env::var("SPIKE_DATA").unwrap_or_else(|_| std::env::temp_dir().join("agentrq-tauri-spike").display().to_string()),
    );
    let timeout: u64 = std::env::var("SPIKE_TIMEOUT").ok().and_then(|v| v.parse().ok()).unwrap_or(90);
    let spike = Arc::new(Spike {
        name: name.clone(),
        server,
        out,
        data,
        started: Instant::now(),
        results: Mutex::new(serde_json::Map::new()),
        profiles: Mutex::new(HashMap::new()),
        sse: Mutex::new(HashMap::new()),
        next_id: AtomicU32::new(1),
    });

    let sp = spike.clone();
    std::thread::spawn(move || {
        std::thread::sleep(Duration::from_secs(timeout));
        sp.write("timeout");
        std::process::exit(2);
    });

    tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
        .manage(spike.clone())
        .register_asynchronous_uri_scheme_protocol("app", |ctx, req, responder| {
            let app = ctx.app_handle().clone();
            let label = ctx.webview_label().to_string();
            tauri::async_runtime::spawn(async move {
                responder.respond(handle(app, label, req).await);
            });
        })
        .invoke_handler(tauri::generate_handler![
            report,
            finish,
            signin_direct,
            jar_clear,
            sse_open,
            sse_close,
            set_profile_cookie,
            panel_open,
            panel_bounds,
            panel_visible,
            panel_info,
            panel_snapshot,
            shot,
            win_pos
        ])
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::Moved(p) = event {
                let spike = window.state::<Arc<Spike>>();
                let s = window.scale_factor().unwrap_or(1.0);
                let l = p.to_logical::<f64>(s);
                let f = spike.out.parent().unwrap().join("s8-pos.json");
                let _ = std::fs::write(f, json!({"x": l.x, "y": l.y}).to_string());
            }
        })
        .setup(move |app| {
            let h = app.handle().clone();
            match spike.name.as_str() {
                "s1" | "s2" => {
                    let page = format!("/s2.html?port={}", spike.server.port().unwrap_or(80));
                    main_window(&h, &spike, "p1", "localhost", &page)?
                }
                "s4" => s4(&h, spike.clone())?,
                "s5a" => s5(&h, spike.clone(), true)?,
                "s5b" => s5(&h, spike.clone(), false)?,
                "s6" => s6(&h, spike.clone())?,
                "s7" => s7(&h, spike.clone())?,
                "bodies" => main_window(&h, &spike, "p1", "localhost", &format!("/bodies.html?{}", std::env::var("Q").unwrap_or_default()))?,
                "s8" => {
                    main_window(&h, &spike, "p1", "localhost", "/s8.html")?;
                    // The driver (scripts/macdrag.swift via run-spike.mjs) says when it is done.
                    let sp = spike.clone();
                    let a = h.clone();
                    std::thread::spawn(move || {
                        let done = sp.out.parent().unwrap().join("s8-done");
                        while !done.exists() {
                            std::thread::sleep(Duration::from_millis(200));
                        }
                        sp.write("done");
                        a.exit(0);
                    });
                }
                "renderer" => main_window(&h, &spike, "p1", "localhost", "/")?,
                other => panic!("unknown SPIKE {other}"),
            }
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
