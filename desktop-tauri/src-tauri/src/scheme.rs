// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The `app` scheme the renderer is served from: `desktop/src/main/protocol.js`'s
//! handler, wired to `core::protocol`'s rules.
//!
//! Every decision is core's: the route (which refuses a traversing path before
//! anything else, on the raw path as the webview sent it), the forwarded URL
//! (built from the cleared path, never `Url::join`ed), the header filters, the
//! budgets, the policy and the replies. This module only moves bytes between
//! the webview, the active profile's client and the bundled renderer.

use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;

use agentrq_desktop_core::attachment_store::{should_cache_response, WriteOptions};
use agentrq_desktop_core::drawer_frame::drawer_frame_document;
use agentrq_desktop_core::protocol::{
    attachment_hit_reply, bad_gateway_reply, dev_failure_message, dev_response_headers,
    dev_unreachable_reply, drawer_code_reply, drawer_frame_reply, filter_request_headers,
    forbidden_reply, not_found_reply, plan_static_request, proxied_response_headers,
    proxy_error_reply, sends_body, serve_from_store, stored_content_disposition, AppProtocol,
    DrawerLookup, HeaderList, ProxyError, Reply, Route, StaticOutcome, PROXY_TIMEOUT_MS,
};
use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use http::{HeaderName, HeaderValue, Request, Response};

use crate::network::{Network, ProfileNet};
use crate::upstream::{self, pairs, BodyLimits, Incoming, Outgoing};
use crate::vault::random;

/// The webview the renderer runs in: the only one whose requests carry the
/// profile's cookies to the server.
pub const MAIN_WEBVIEW: &str = "main";

/// The built renderer. Paths are `/`-rooted and already decoded.
pub trait Assets: Send + Sync {
    /// Only a real file: never the SPA fallback Tauri's own resolver applies.
    fn exists(&self, path: &str) -> bool;
    fn read(&self, path: &str) -> Option<Vec<u8>>;
}

/// What draws an extension's format (WP9): the frame's origin, which its
/// policy names, and a drawer's code by format.
pub trait Drawers: Send + Sync {
    fn origin(&self) -> String;
    fn code(
        &self,
        format: Option<String>,
    ) -> Pin<Box<dyn Future<Output = DrawerLookup> + Send + '_>>;
}

/// Told about every request forwarded to the server, before it is sent: the
/// notification self-action gate (WP6) marks the human's own replies with it.
pub type OnProxied = Box<dyn Fn(&str, &str) + Send + Sync>;

pub struct Scheme {
    network: Arc<Network>,
    assets: Box<dyn Assets>,
    /// Vite, in `tauri dev`; empty in a build.
    dev_server_url: String,
    on_proxied: OnProxied,
    /// For Vite only: it is not the server, so it gets no profile's cookies.
    dev_client: reqwest::Client,
    limits: BodyLimits,
    drawers: Option<Box<dyn Drawers>>,
}

impl Scheme {
    pub fn new(network: Arc<Network>, assets: Box<dyn Assets>, dev_server_url: &str) -> Self {
        Self {
            network,
            assets,
            dev_server_url: dev_server_url.to_string(),
            on_proxied: Box::new(|_, _| {}),
            dev_client: reqwest::Client::new(),
            limits: BodyLimits::default(),
            drawers: None,
        }
    }

    pub fn drawers(mut self, drawers: Box<dyn Drawers>) -> Self {
        self.drawers = Some(drawers);
        self
    }

    #[allow(dead_code)] // WP6 feeds the notification self-action gate.
    pub fn on_proxied(mut self, hook: OnProxied) -> Self {
        self.on_proxied = hook;
        self
    }

    /// Answer `request`, made by the webview labelled `webview`. Only a proxied
    /// request opens the profile, which reads its key from disk: the renderer
    /// itself never does. Any other webview (a side panel guest) gets nothing,
    /// not even the renderer: the scheme is app-wide, and a page in the panel
    /// could otherwise frame the app.
    pub async fn handle(&self, webview: &str, request: Request<Vec<u8>>) -> Response<Vec<u8>> {
        if webview != MAIN_WEBVIEW {
            return text(forbidden_reply());
        }
        let protocol = self.network.protocol(&self.dev_server_url);
        let path = request.uri().path().to_string();
        let search = request
            .uri()
            .query()
            .map(|query| format!("?{query}"))
            .unwrap_or_default();
        match protocol.route(&path) {
            Route::Forbidden => text(forbidden_reply()),
            Route::DrawerFrame => text(match &self.drawers {
                // The header and the document carry one nonce, made here.
                Some(drawers) => {
                    let nonce = STANDARD.encode(random::<16>());
                    let (html, csp) = drawer_frame_document(&nonce, &drawers.origin());
                    drawer_frame_reply(&html, &csp)
                }
                None => not_found_reply(),
            }),
            Route::DrawerCode(format) => text(drawer_code_reply(&match &self.drawers {
                Some(drawers) => drawers.code(format).await,
                None => DrawerLookup::default(),
            })),
            Route::Proxy => {
                let net = self.network.active().await;
                let protocol = net.protocol(&self.dev_server_url);
                self.proxy(&net, &protocol, request, &path, &search).await
            }
            Route::DevServer => self.dev_server(&protocol, &path, &search).await,
            Route::Static => self.bundled(&protocol, &path),
        }
    }

    async fn proxy(
        &self,
        net: &Arc<ProfileNet>,
        protocol: &AppProtocol<impl Fn() -> String>,
        request: Request<Vec<u8>>,
        path: &str,
        search: &str,
    ) -> Response<Vec<u8>> {
        let method = request.method().clone();
        let cached = serve_from_store(method.as_str(), path);
        if cached {
            if let Some(hit) = net.attachments.read(path) {
                let reply =
                    attachment_hit_reply(hit.body, &hit.content_type, &hit.content_disposition);
                return respond(reply.status, &reply.headers, reply.body);
            }
        }

        (self.on_proxied)(method.as_str(), path);
        let url = match protocol.proxy_target(path, search) {
            Ok(url) => url,
            Err(error) => return text(proxy_error_reply(&error)),
        };
        let has_body = sends_body(method.as_str(), !request.body().is_empty());
        let headers = request
            .headers()
            .iter()
            .filter_map(|(name, value)| Some((name.as_str(), value.to_str().ok()?)))
            .collect::<Vec<_>>();
        let out = Outgoing {
            method,
            url,
            headers: filter_request_headers(headers),
            body: has_body.then(|| request.into_body()),
            timeout_ms: protocol.timeout_ms(has_body),
            limits: self.limits,
        };
        let mut answer = match upstream::send(&net.client, out).await {
            Ok(answer) => answer,
            Err(error) => return text(proxy_error_reply(&error)),
        };
        let body = match std::mem::replace(&mut answer.body, Ok(Vec::new())) {
            Ok(body) => body,
            Err(ProxyError::Unreachable(_)) if cached => return text(bad_gateway_reply()),
            Err(error) => return text(proxy_error_reply(&error)),
        };
        // Only a successful body is worth keeping: an error page cached under an
        // attachment's name would outlive whatever caused it.
        if cached && should_cache_response(answer.status) {
            // A store that cannot be written behaves like one never written.
            net.attachments.write(
                path,
                &body,
                WriteOptions {
                    content_type: answer.header("content-type").unwrap_or_default(),
                    content_disposition: stored_content_disposition(
                        answer.header("content-disposition"),
                    ),
                    size: Some(body.len() as u64),
                },
            );
        }
        let headers = proxied_response_headers(pairs(&answer.headers));
        respond(answer.status, &headers, body)
    }

    async fn dev_server(
        &self,
        protocol: &AppProtocol<impl Fn() -> String>,
        path: &str,
        search: &str,
    ) -> Response<Vec<u8>> {
        let url = match protocol.dev_target(path, search) {
            Ok(url) => url,
            Err(message) => return text(dev_unreachable_reply(&message)),
        };
        let out = Outgoing {
            method: http::Method::GET,
            url,
            headers: Vec::new(),
            body: None,
            timeout_ms: PROXY_TIMEOUT_MS,
            limits: self.limits,
        };
        let failure = match upstream::send(&self.dev_client, out).await {
            Ok(Incoming {
                status,
                headers,
                body: Ok(body),
            }) => {
                let headers = dev_response_headers(pairs(&headers), &|| protocol.csp());
                return respond(status, &headers, body);
            }
            Ok(Incoming {
                body: Err(error), ..
            })
            | Err(error) => error,
        };
        text(dev_unreachable_reply(&dev_failure_message(&failure)))
    }

    fn bundled(
        &self,
        protocol: &AppProtocol<impl Fn() -> String>,
        path: &str,
    ) -> Response<Vec<u8>> {
        let serve = match plan_static_request(path, &|file| self.assets.exists(file)) {
            StaticOutcome::Forbidden => return text(forbidden_reply()),
            StaticOutcome::NotFound => return text(not_found_reply()),
            StaticOutcome::Serve(serve) => serve,
        };
        match self.assets.read(&serve.file_path) {
            Some(body) => respond(200, &serve.headers(&|| protocol.csp()), body),
            None => text(not_found_reply()),
        }
    }
}

fn text(reply: Reply) -> Response<Vec<u8>> {
    respond(reply.status, &reply.headers, reply.body.into_bytes())
}

/// A response from core's parts. A header the `http` crate refuses is left
/// out rather than failing the whole answer.
fn respond(status: u16, headers: &HeaderList, body: Vec<u8>) -> Response<Vec<u8>> {
    let mut response = Response::new(body);
    *response.status_mut() =
        http::StatusCode::from_u16(status).unwrap_or(http::StatusCode::BAD_GATEWAY);
    for (name, value) in headers {
        if let (Ok(name), Ok(value)) = (
            HeaderName::from_bytes(name.as_bytes()),
            HeaderValue::from_str(value),
        ) {
            response.headers_mut().append(name, value);
        }
    }
    response
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::network::tests::network;
    use crate::test_server::{closed_port, reply, send, Seen, Step, TestServer};
    use std::collections::HashMap;
    use std::sync::Mutex;

    const ATTACHMENT: &str = "/api/v1/workspaces/ws1/tasks/t1/attachments/a1";

    /// A renderer of a few files.
    struct Files(HashMap<&'static str, &'static str>);

    impl Assets for Files {
        fn exists(&self, path: &str) -> bool {
            self.0.contains_key(path)
        }
        fn read(&self, path: &str) -> Option<Vec<u8>> {
            self.0
                .get(path)
                .filter(|body| !body.is_empty())
                .map(|body| body.as_bytes().to_vec())
        }
    }

    fn renderer() -> Box<Files> {
        Box::new(Files(HashMap::from([
            ("/index.html", "<html>app</html>"),
            ("/assets/main.js", "js"),
            ("/assets/a b.js", "spaced"),
            // Listed but unreadable: a resolver that changed its mind.
            ("/gone.js", ""),
        ])))
    }

    struct Rig {
        scheme: Scheme,
        _dir: tempfile::TempDir,
        proxied: Arc<Mutex<Vec<(String, String)>>>,
    }

    fn rig(server_url: &str, dev_server_url: &str) -> Rig {
        rig_in(tempfile::tempdir().unwrap(), server_url, dev_server_url)
    }

    fn rig_in(dir: tempfile::TempDir, server_url: &str, dev_server_url: &str) -> Rig {
        let mut files = renderer();
        files.0.remove("/gone.js");
        let scheme = Scheme::new(
            Arc::new(network(dir.path(), server_url)),
            files,
            dev_server_url,
        );
        let proxied = Arc::new(Mutex::new(Vec::new()));
        let log = proxied.clone();
        let scheme = scheme.on_proxied(Box::new(move |method, path| {
            log.lock()
                .unwrap()
                .push((method.to_string(), path.to_string()));
        }));
        Rig {
            scheme,
            _dir: dir,
            proxied,
        }
    }

    fn request(method: &str, uri: &str, body: &[u8]) -> Request<Vec<u8>> {
        Request::builder()
            .method(method)
            .uri(format!("app://localhost{uri}"))
            .header("origin", "app://localhost")
            .header("referer", "app://localhost/")
            .header("content-type", "application/json")
            .header("x-binary", HeaderValue::from_bytes(b"caf\xe9").unwrap())
            .body(body.to_vec())
            .unwrap()
    }

    async fn get(rig: &Rig, uri: &str) -> Response<Vec<u8>> {
        rig.scheme
            .handle(MAIN_WEBVIEW, request("GET", uri, b""))
            .await
    }

    fn header<'a>(res: &'a Response<Vec<u8>>, name: &str) -> Option<&'a str> {
        res.headers().get(name).map(|v| v.to_str().unwrap())
    }

    fn body(res: &Response<Vec<u8>>) -> String {
        String::from_utf8_lossy(res.body()).to_string()
    }

    fn echo(seen: &Seen) -> Vec<Step> {
        let text = format!(
            "{} {} {}",
            seen.method,
            seen.target,
            String::from_utf8_lossy(&seen.body)
        );
        reply(
            "200 OK",
            &[
                "content-type: text/plain",
                "set-cookie: at=secret; Path=/; Max-Age=60",
                "access-control-allow-origin: *",
                "x-upstream: yes",
            ],
            &text,
        )
    }

    // proxying

    #[tokio::test]
    async fn forwards_an_api_request_to_the_configured_server_preserving_the_query() {
        let server = TestServer::start(echo).await;
        let rig = rig(&server.url, "");
        let res = get(&rig, "/api/v1/workspaces?limit=5").await;
        assert_eq!(res.status(), 200);
        assert_eq!(body(&res), "GET /api/v1/workspaces?limit=5 ");
        assert_eq!(header(&res, "x-upstream"), Some("yes"));
    }

    #[tokio::test]
    async fn forwards_a_request_body_and_drops_the_app_origin_headers() {
        let server = TestServer::start(echo).await;
        let rig = rig(&server.url, "");
        let res = rig
            .scheme
            .handle(MAIN_WEBVIEW, request("POST", "/mcp/ws1", b"{\"a\":1}"))
            .await;
        assert_eq!(body(&res), "POST /mcp/ws1 {\"a\":1}");
        let seen = &server.requests()[0];
        assert_eq!(seen.header("origin"), None);
        assert_eq!(seen.header("referer"), None);
        assert_eq!(seen.header("content-type"), Some("application/json"));
        assert_eq!(
            seen.header("x-binary"),
            None,
            "not a string, so not forwarded"
        );
    }

    #[tokio::test]
    async fn sends_no_body_for_get_and_head_and_omits_an_empty_one() {
        let server = TestServer::start(echo).await;
        let rig = rig(&server.url, "");
        for method in ["GET", "HEAD"] {
            rig.scheme
                .handle(MAIN_WEBVIEW, request(method, "/api/v1/x", b"ignored"))
                .await;
        }
        rig.scheme
            .handle(MAIN_WEBVIEW, request("POST", "/api/v1/x", b""))
            .await;
        let seen = server.requests();
        assert_eq!(
            seen.iter().map(|s| s.method.as_str()).collect::<Vec<_>>(),
            ["GET", "HEAD", "POST"]
        );
        for seen in seen {
            assert!(seen.body.is_empty());
            assert_eq!(seen.header("content-length").unwrap_or("0"), "0");
        }
    }

    #[tokio::test]
    async fn keeps_set_cookie_in_the_profile_jar_and_away_from_the_renderer() {
        let server = TestServer::start(echo).await;
        let rig = rig(&server.url, "");
        let res = get(&rig, "/api/v1/auth/x").await;
        assert_eq!(header(&res, "set-cookie"), None);
        assert_eq!(header(&res, "access-control-allow-origin"), None);
        assert_eq!(header(&res, "content-length"), None);

        get(&rig, "/api/v1/workspaces").await;
        assert_eq!(server.requests()[1].header("cookie"), Some("at=secret"));
    }

    #[tokio::test]
    async fn handles_redirects_itself_rather_than_following_them() {
        let server =
            TestServer::start(|_| reply("302 Found", &["location: /api/v1/next"], "")).await;
        let rig = rig(&server.url, "");
        let res = get(&rig, "/api/v1/auth/github").await;
        assert_eq!(res.status(), 302);
        assert_eq!(header(&res, "location"), Some("/api/v1/next"));
        assert_eq!(server.requests().len(), 1);
    }

    #[tokio::test]
    async fn reports_503_when_no_server_has_been_configured_yet() {
        let rig = rig("", "");
        let res = get(&rig, "/api/v1/auth/config").await;
        assert_eq!(res.status(), 503);
        assert_eq!(body(&res), r#"{"error":"No AgentRQ server is configured"}"#);
    }

    #[tokio::test]
    async fn reports_an_unreachable_server_as_502() {
        let rig = rig(&closed_port().await, "");
        let res = get(&rig, "/api/v1/x").await;
        assert_eq!(res.status(), 502);
        assert!(body(&res).starts_with(r#"{"error":"Cannot reach the AgentRQ server","detail":"#));
    }

    #[tokio::test]
    async fn reports_a_body_that_broke_off_as_502() {
        let server =
            TestServer::start(|_| vec![send("HTTP/1.1 200 OK\r\ncontent-length: 9\r\n\r\nab")])
                .await;
        let rig = rig(&server.url, "");
        assert_eq!(get(&rig, "/api/v1/x").await.status(), 502);
    }

    #[tokio::test]
    async fn answers_504_rather_than_hanging_when_the_response_never_arrives() {
        let server = TestServer::start(|_| vec![Step::Wait(60_000)]).await;
        let rig = rig(&server.url, "");
        let res = get(&rig, "/api/v1/slow");
        tokio::pin!(res);
        // Real time until the server has the request: a paused clock jumps
        // ahead while the sockets are busy, and the deadline could pass first.
        let started = std::time::Instant::now();
        while server.requests().is_empty() {
            assert!(
                started.elapsed() < std::time::Duration::from_secs(10),
                "the server never got the request"
            );
            tokio::select! {
                res = &mut res => panic!("answered {} before the server had it", res.status()),
                _ = tokio::time::sleep(std::time::Duration::from_millis(10)) => {}
            }
        }
        tokio::time::pause();
        assert_eq!(res.await.status(), 504);
    }

    #[tokio::test]
    async fn answers_504_when_the_body_stops_arriving_after_the_headers() {
        // Real time: a paused clock jumps ahead while the socket is still busy.
        let server = TestServer::start(|_| {
            vec![
                send("HTTP/1.1 200 OK\r\ncontent-type: image/png\r\ncontent-length: 9\r\n\r\nab"),
                Step::Wait(5_000),
            ]
        })
        .await;
        let mut rig = rig(&server.url, "");
        rig.scheme.limits.idle_ms = 150;
        for path in ["/api/v1/x", ATTACHMENT] {
            let res = get(&rig, path).await;
            assert_eq!(res.status(), 504, "{path}");
            assert_eq!(
                body(&res),
                r#"{"error":"The AgentRQ server stopped sending its response"}"#
            );
        }
    }

    #[tokio::test]
    async fn answers_502_for_a_body_over_the_cap() {
        let server = TestServer::start(echo).await;
        let mut rig = rig(&server.url, "");
        rig.scheme.limits.max_bytes = 4;
        let res = get(&rig, "/api/v1/x").await;
        assert_eq!(res.status(), 502);
        assert_eq!(
            body(&res),
            r#"{"error":"The AgentRQ server's response is too large"}"#
        );
    }

    #[tokio::test]
    async fn sandboxes_every_forwarded_response() {
        let server = TestServer::start(|_| {
            reply(
                "200 OK",
                &[
                    "content-type: text/html;pdf",
                    "content-security-policy: default-src *",
                    "keep-alive: timeout=5",
                ],
                "<script>parent.__TAURI_INTERNALS__</script>",
            )
        })
        .await;
        let rig = rig(&server.url, "");
        for _ in 0..2 {
            let res = get(&rig, ATTACHMENT).await;
            assert_eq!(header(&res, "content-type"), Some("text/html;pdf"));
            assert_eq!(
                header(&res, "content-security-policy"),
                Some("sandbox; default-src 'none'")
            );
            assert_eq!(header(&res, "x-content-type-options"), Some("nosniff"));
            assert_eq!(header(&res, "keep-alive"), None);
        }
        assert_eq!(server.requests().len(), 1, "the second was the cache's");
    }

    #[tokio::test]
    async fn answers_another_webview_with_nothing_at_all() {
        let server = TestServer::start(echo).await;
        let rig = rig(&server.url, "");
        let res = rig
            .scheme
            .handle("side-panel", request("GET", "/api/v1/workspaces", b""))
            .await;
        assert_eq!(res.status(), 403);
        assert!(server.requests().is_empty());
        let page = rig
            .scheme
            .handle("panel-guest-1", request("GET", "/", b""))
            .await;
        assert_eq!(page.status(), 403);
    }

    #[tokio::test]
    async fn serves_the_renderer_without_opening_a_profile() {
        let dir = tempfile::tempdir().unwrap();
        let network = crate::network::tests::network(dir.path(), "https://a.example");
        let scheme = Scheme::new(Arc::new(network), renderer(), "");
        let res = scheme.handle(MAIN_WEBVIEW, request("GET", "/", b"")).await;
        assert_eq!(body(&res), "<html>app</html>");
        let csp = header(&res, "content-security-policy").unwrap();
        assert!(csp.contains("wss://a.example"), "{csp}");
        assert!(
            !dir.path().join("cookie.key").exists(),
            "no key read for it"
        );
    }

    #[tokio::test]
    async fn reports_every_forwarded_request_to_on_proxied_before_fetching() {
        let rig = rig("", "");
        rig.scheme
            .handle(
                MAIN_WEBVIEW,
                request("POST", "/api/v1/workspaces/w/tasks/t/reply", b"x"),
            )
            .await;
        assert_eq!(
            *rig.proxied.lock().unwrap(),
            [(
                "POST".to_string(),
                "/api/v1/workspaces/w/tasks/t/reply".to_string()
            )],
            "even when the request then fails"
        );
    }

    #[tokio::test]
    async fn defaults_to_a_no_op_hook() {
        let dir = tempfile::tempdir().unwrap();
        let scheme = Scheme::new(Arc::new(network(dir.path(), "")), renderer(), "");
        (scheme.on_proxied)("GET", "/api/v1/x");
        assert_eq!(
            scheme
                .handle(MAIN_WEBVIEW, request("GET", "/api/v1/x", b""))
                .await
                .status(),
            503
        );
    }

    #[tokio::test]
    async fn refuses_traversal_before_anything_is_routed() {
        let server = TestServer::start(echo).await;
        let rig = rig(&server.url, "");
        for path in ["/api/../x", "/api/%2e%2e/healthz", "/assets/..%5cmain.js"] {
            assert_eq!(get(&rig, path).await.status(), 403, "{path}");
        }
        assert!(server.requests().is_empty());
        assert!(rig.proxied.lock().unwrap().is_empty());
    }

    // drawers

    struct FakeDrawers;

    impl Drawers for FakeDrawers {
        fn origin(&self) -> String {
            "app://localhost".into()
        }
        fn code(
            &self,
            format: Option<String>,
        ) -> Pin<Box<dyn Future<Output = DrawerLookup> + Send + '_>> {
            Box::pin(async move {
                match format.as_deref() {
                    Some("mermaid") => DrawerLookup::Found {
                        code: "export default 1".into(),
                    },
                    _ => DrawerLookup::Refused { reason: None },
                }
            })
        }
    }

    #[tokio::test]
    async fn serves_the_frame_with_one_fresh_nonce_and_a_drawer_by_format() {
        let mut rig = rig("", "");
        rig.scheme = rig.scheme.drawers(Box::new(FakeDrawers));
        let frame = get(&rig, "/__agentrq/drawer-frame.html").await;
        assert_eq!(frame.status(), 200);
        let csp = header(&frame, "content-security-policy")
            .unwrap()
            .to_string();
        assert!(
            csp.contains("script-src 'nonce-") && csp.contains(" app://localhost"),
            "{csp}"
        );
        let nonce = csp
            .split("'nonce-")
            .nth(1)
            .unwrap()
            .split('\'')
            .next()
            .unwrap();
        assert_eq!(nonce.len(), 24);
        assert!(body(&frame).contains(&format!("nonce=\"{nonce}\"")));
        let again = get(&rig, "/__agentrq/drawer-frame.html").await;
        assert_ne!(header(&again, "content-security-policy").unwrap(), csp);

        let code = get(&rig, "/__agentrq/drawer/mermaid.js").await;
        assert_eq!(code.status(), 200);
        assert_eq!(body(&code), "export default 1");
        assert_eq!(header(&code, "access-control-allow-origin"), Some("*"));
        let none = get(&rig, "/__agentrq/drawer/vega.js").await;
        assert_eq!(
            (none.status().as_u16(), body(&none).as_str()),
            (404, "No such drawer")
        );
    }

    #[tokio::test]
    async fn answers_without_a_drawer_source_at_all() {
        let rig = rig("", "");
        let frame = get(&rig, "/__agentrq/drawer-frame.html").await;
        assert_eq!(frame.status(), 404);
        let code = get(&rig, "/__agentrq/drawer/mermaid.js").await;
        assert_eq!(code.status(), 404);
        assert_eq!(body(&code), "Extensions are unavailable.");
    }

    // bundled assets

    #[tokio::test]
    async fn serves_index_html_at_the_root_with_a_csp_and_no_caching() {
        let rig = rig("https://agentrq.example", "");
        let res = get(&rig, "/").await;
        assert_eq!(res.status(), 200);
        assert_eq!(body(&res), "<html>app</html>");
        assert_eq!(
            header(&res, "content-type"),
            Some("text/html; charset=utf-8")
        );
        assert_eq!(header(&res, "cache-control"), Some("no-store"));
        let csp = header(&res, "content-security-policy").unwrap();
        assert!(csp.contains("connect-src 'self'"));
        assert!(csp.contains("wss://agentrq.example"), "{csp}");
    }

    #[tokio::test]
    async fn serves_an_existing_asset_with_its_own_type_and_no_csp_header() {
        let rig = rig("", "");
        let res = get(&rig, "/assets/main.js").await;
        assert_eq!(body(&res), "js");
        assert_eq!(
            header(&res, "content-type"),
            Some("text/javascript; charset=utf-8")
        );
        assert_eq!(header(&res, "content-security-policy"), None);
        assert_eq!(header(&res, "cache-control"), None);
    }

    #[tokio::test]
    async fn falls_back_to_index_html_for_a_deep_spa_route_and_404s_a_missing_asset() {
        let rig = rig("", "");
        assert_eq!(
            body(&get(&rig, "/workspaces/1/tasks").await),
            "<html>app</html>"
        );
        assert_eq!(get(&rig, "/assets/missing.js").await.status(), 404);
    }

    #[tokio::test]
    async fn decodes_the_path_and_refuses_what_must_not_reach_the_disk() {
        let rig = rig("", "");
        assert_eq!(body(&get(&rig, "/assets/a%20b.js").await), "spaced");
        assert_eq!(get(&rig, "/assets/%00.js").await.status(), 403);
        assert_eq!(get(&rig, "/assets/%zz.js").await.status(), 403);
        assert_eq!(
            get(&rig, "/assets/con.js").await.status(),
            403,
            "a Windows device"
        );
    }

    #[tokio::test]
    async fn answers_404_for_a_file_that_cannot_be_read() {
        let dir = tempfile::tempdir().unwrap();
        let scheme = Scheme::new(Arc::new(network(dir.path(), "")), renderer(), "");
        assert_eq!(
            scheme
                .handle(MAIN_WEBVIEW, request("GET", "/gone.js", b""))
                .await
                .status(),
            404
        );
    }

    // dev server

    #[tokio::test]
    async fn sources_assets_from_vite_with_the_dev_policy_while_still_proxying_the_api() {
        let vite = TestServer::start(|seen| {
            if seen.target.starts_with("/src") {
                reply("200 OK", &["content-type: text/javascript"], "module")
            } else {
                reply(
                    "200 OK",
                    &["content-type: text/html", "content-security-policy: vite's"],
                    "<html>dev</html>",
                )
            }
        })
        .await;
        let server = TestServer::start(echo).await;
        let rig = rig(&server.url, &vite.url);

        let page = get(&rig, "/workspaces?x=1").await;
        assert_eq!(body(&page), "<html>dev</html>");
        let csp = header(&page, "content-security-policy").unwrap();
        assert!(csp.contains("'unsafe-eval'"), "{csp}");
        assert_eq!(vite.requests()[0].target, "/workspaces?x=1");

        let module = get(&rig, "/src/main.js").await;
        assert_eq!(header(&module, "content-security-policy"), None);

        assert_eq!(body(&get(&rig, "/api/v1/x").await), "GET /api/v1/x ");
    }

    #[tokio::test]
    async fn answers_502_rather_than_failing_when_the_dev_server_is_down_or_broken() {
        let down = rig("", &closed_port().await);
        let res = get(&down, "/").await;
        assert_eq!(res.status(), 502);
        assert!(body(&res).starts_with("Cannot reach the dev server: "));

        let broken =
            TestServer::start(|_| vec![send("HTTP/1.1 200 OK\r\ncontent-length: 9\r\n\r\nab")])
                .await;
        assert_eq!(
            get(&rig_in(tempfile::tempdir().unwrap(), "", &broken.url), "/")
                .await
                .status(),
            502
        );

        let invalid = rig("", "not a url");
        assert_eq!(
            body(&get(&invalid, "/").await),
            "Cannot reach the dev server: Invalid URL"
        );
    }

    #[tokio::test]
    async fn answers_502_rather_than_hanging_when_the_dev_server_never_replies() {
        let vite = TestServer::start(|_| vec![Step::Wait(60_000)]).await;
        let rig = rig("", &vite.url);
        let res = get(&rig, "/");
        tokio::pin!(res);
        // Real time until Vite has the request, as in the 504 test above: a
        // clock paused sooner can pass the deadline before Vite reads it.
        let started = std::time::Instant::now();
        while vite.requests().is_empty() {
            assert!(
                started.elapsed() < std::time::Duration::from_secs(10),
                "Vite never got the request"
            );
            tokio::select! {
                res = &mut res => panic!("answered {} before Vite had it", res.status()),
                _ = tokio::time::sleep(std::time::Duration::from_millis(10)) => {}
            }
        }
        tokio::time::pause();
        let res = res.await;
        assert_eq!(res.status(), 502);
        assert_eq!(
            body(&res),
            "Cannot reach the dev server: no response headers after 30000ms"
        );
    }

    // attachments

    fn png(_: &Seen) -> Vec<Step> {
        reply(
            "200 OK",
            &[
                "content-type: image/png",
                "content-disposition: inline; filename=\"a.png\"",
            ],
            "PNG",
        )
    }

    #[tokio::test]
    async fn forwards_a_miss_keeps_what_comes_back_and_serves_it_without_asking_again() {
        let server = TestServer::start(png).await;
        let rig = rig(&server.url, "");
        let first = get(&rig, ATTACHMENT).await;
        assert_eq!(body(&first), "PNG");

        let second = get(&rig, ATTACHMENT).await;
        assert_eq!(second.status(), 200);
        assert_eq!(body(&second), "PNG");
        assert_eq!(header(&second, "content-type"), Some("image/png"));
        assert_eq!(
            header(&second, "content-disposition"),
            Some("inline; filename=\"a.png\"")
        );
        assert_eq!(server.requests().len(), 1);
        assert_eq!(
            rig.proxied.lock().unwrap().len(),
            1,
            "a hit is not forwarded"
        );
    }

    #[tokio::test]
    async fn keeps_nothing_when_the_server_did_not_answer_with_the_file() {
        let server = TestServer::start(|_| {
            reply(
                "404 Not Found",
                &["content-type: text/plain"],
                "attachment not found",
            )
        })
        .await;
        let rig = rig(&server.url, "");
        assert_eq!(get(&rig, ATTACHMENT).await.status(), 404);
        assert_eq!(get(&rig, ATTACHMENT).await.status(), 404);
        assert_eq!(server.requests().len(), 2);
    }

    #[tokio::test]
    async fn reports_a_bad_gateway_when_the_body_fails_mid_read() {
        let server = TestServer::start(|_| {
            vec![send(
                "HTTP/1.1 200 OK\r\ncontent-type: image/png\r\ncontent-length: 9\r\n\r\nab",
            )]
        })
        .await;
        let rig = rig(&server.url, "");
        let res = get(&rig, ATTACHMENT).await;
        assert_eq!(res.status(), 502);
        assert_eq!(body(&res), "Bad Gateway");
    }

    #[tokio::test]
    async fn keeps_a_response_that_arrived_without_a_disposition_but_not_one_with_no_type() {
        let server = TestServer::start(|seen| {
            if seen.target.ends_with("a1") {
                reply("200 OK", &["content-type: image/png"], "PNG")
            } else {
                reply("200 OK", &[], "raw")
            }
        })
        .await;
        let rig = rig(&server.url, "");
        get(&rig, ATTACHMENT).await;
        let hit = get(&rig, ATTACHMENT).await;
        assert_eq!(header(&hit, "content-disposition"), None);

        let untyped = "/api/v1/workspaces/ws1/tasks/t1/attachments/a2";
        get(&rig, untyped).await;
        get(&rig, untyped).await;
        assert_eq!(
            server.requests().len(),
            3,
            "no type, nothing to serve it as"
        );
    }

    #[tokio::test]
    async fn still_serves_when_the_store_itself_fails() {
        let server = TestServer::start(png).await;
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(dir.path().join("profiles").join("default")).unwrap();
        std::fs::write(
            dir.path()
                .join("profiles")
                .join("default")
                .join("attachments"),
            b"x",
        )
        .unwrap();
        let rig = rig_in(dir, &server.url, "");
        assert_eq!(body(&get(&rig, ATTACHMENT).await), "PNG");
        assert_eq!(body(&get(&rig, ATTACHMENT).await), "PNG");
        assert_eq!(server.requests().len(), 2);
    }

    #[tokio::test]
    async fn does_not_intercept_a_write_to_an_attachment_path_or_any_other_request() {
        let server = TestServer::start(png).await;
        let rig = rig(&server.url, "");
        rig.scheme
            .handle(MAIN_WEBVIEW, request("POST", ATTACHMENT, b"x"))
            .await;
        rig.scheme
            .handle(MAIN_WEBVIEW, request("POST", ATTACHMENT, b"x"))
            .await;
        get(&rig, "/api/v1/workspaces/ws1/tasks").await;
        get(&rig, "/api/v1/workspaces/ws1/tasks").await;
        assert_eq!(server.requests().len(), 4);
    }

    #[test]
    fn drops_a_header_http_refuses_and_a_status_it_cannot_carry() {
        let res = respond(
            1000,
            &vec![
                ("x-ok".into(), "1".into()),
                ("bad name".into(), "x".into()),
                ("x-bad".into(), "a\nb".into()),
            ],
            Vec::new(),
        );
        assert_eq!(res.status(), 502);
        assert_eq!(res.headers().len(), 1);
    }
}
