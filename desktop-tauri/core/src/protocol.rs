// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The `app://` protocol's rules: what is forwarded to the AgentRQ server, what
//! is served from the bundled renderer, and the policy the renderer runs under.
//!
//! The web frontend addresses the API with same-origin *relative* URLs and
//! authenticates with the `at` cookie. A renderer loaded from a file would make
//! every one of those calls cross-origin and the cookie would never be
//! attached. So the renderer is served from a privileged scheme and everything
//! under `/api`, `/mcp` and `/.well-known` is forwarded to the configured server
//! from the main process. The renderer only ever sees same-origin traffic.
//!
//! Only the decisions live here. The network, the filesystem and the shell's
//! `protocol`/`net` calls are the caller's: it asks this module *what* to do
//! (which route, which URL, which headers, which status) and does it.
//!
//! Rust notes:
//! - Nothing here reads a `Request`. A path is a `&str`, a header list is a
//!   slice of pairs, and a body is a `bool` (is there one).
//! - Header names are lowercased on the way out, as `Headers` does.
//! - Where the JS handler lets an exception escape (a malformed escape in a
//!   drawer name, a configured server that is not a URL) this answers with a
//!   status instead, because a handler that never settles is the freeze the JS
//!   code elsewhere goes out of its way to avoid.
//! - A Tauri request path is **not** dot-collapsed the way a WHATWG `URL`
//!   pathname is, so [`traverses`] is the primary control there, not defence in
//!   depth. It runs before any routing decision, and the forwarded URL is built
//!   from the path it cleared rather than resolved with `Url::join`, which
//!   would collapse a `..` *after* the allow-list had already said yes. (The JS
//!   has that flaw in `isProxyPath`; this is a deliberate divergence.)

use crate::uri::decode_uri_component;
use url::Url;

/// Path prefixes forwarded to the AgentRQ server. These mirror the backend's
/// own routing; `/mcp` has no trailing slash there, so both `/mcp` and
/// `/mcp/<workspace>` match.
pub const PROXY_PREFIXES: [&str; 3] = ["/api/", "/mcp", "/.well-known/"];

/// The document an extension's drawer runs in. Not a renderer asset.
pub const DRAWER_FRAME_PATH: &str = "/__agentrq/drawer-frame.html";

/// A drawer's code, for the frame to import: `<prefix><format>.js`.
pub const DRAWER_CODE_PREFIX: &str = "/__agentrq/drawer/";

/// How long a forwarded request may take to produce *response headers*.
///
/// A request that never settles is worse than one that fails: the renderer's
/// own fetch waits on it forever and nothing is ever reported. The budget
/// covers headers only, so a long-lived body (the event stream) is never cut.
pub const PROXY_TIMEOUT_MS: u64 = 30_000;

/// The same budget for a request that carries a body, longer because the
/// server cannot answer until it has read the upload.
pub const UPLOAD_TIMEOUT_MS: u64 = 120_000;

/// How long a forwarded response's body may go without a byte once its headers
/// are in. The headers budget ends at the headers, and a connection that dies
/// after them would otherwise hold the renderer's `fetch` for the life of the
/// page.
pub const PROXY_IDLE_TIMEOUT_MS: u64 = 30_000;

/// The most a forwarded response's body may hold: the shell buffers it whole.
pub const MAX_PROXIED_BODY_BYTES: u64 = 128 * 1024 * 1024;

/// Request headers that must not be forwarded to the server: the first three
/// carry the `app://` origin, and the length describes a body the shell
/// re-frames.
const STRIPPED_REQUEST_HEADERS: [&str; 4] = ["origin", "referer", "host", "content-length"];

/// Hop-by-hop headers (RFC 9110 7.6.1), which describe one connection and
/// never cross a proxy, in either direction: the shell's client frames its own
/// request, and the renderer gets a body already de-chunked. Any `proxy-*`
/// header, and any header a `connection` header names, is one too.
const HOP_BY_HOP_HEADERS: [&str; 6] = [
    "connection",
    "keep-alive",
    "te",
    "trailer",
    "transfer-encoding",
    "upgrade",
];

/// Response headers that must not be forwarded to the renderer.
const STRIPPED_RESPONSE_HEADERS: [&str; 8] = [
    // The cookie jar in the main process already stored these against the real
    // server host; replaying them at the app origin would fork the session.
    "set-cookie",
    // The body has already been decoded, so these would describe bytes the
    // renderer never receives.
    "content-encoding",
    "content-length",
    // Meaningless once the renderer sees the response as same-origin.
    "access-control-allow-origin",
    "access-control-allow-credentials",
    "access-control-allow-headers",
    "access-control-allow-methods",
    "access-control-expose-headers",
];

/// A header list, in order, names lowercased.
pub type HeaderList = Vec<(String, String)>;

fn filter_headers<'a>(
    headers: impl IntoIterator<Item = (&'a str, &'a str)>,
    stripped: &[&str],
) -> HeaderList {
    let headers: HeaderList = headers
        .into_iter()
        .map(|(name, value)| (name.to_ascii_lowercase(), value.to_string()))
        .collect();
    let named_by_connection: Vec<String> = headers
        .iter()
        .filter(|(name, _)| name == "connection")
        .flat_map(|(_, value)| value.split(','))
        .map(|token| token.trim().to_ascii_lowercase())
        .collect();
    headers
        .into_iter()
        .filter(|(name, _)| {
            !stripped.contains(&name.as_str())
                && !HOP_BY_HOP_HEADERS.contains(&name.as_str())
                && !name.starts_with("proxy-")
                && !named_by_connection.contains(name)
        })
        .collect()
}

/// The headers to send to the server: everything but the app origin's.
pub fn filter_request_headers<'a>(
    headers: impl IntoIterator<Item = (&'a str, &'a str)>,
) -> HeaderList {
    filter_headers(headers, &STRIPPED_REQUEST_HEADERS)
}

/// The headers to hand the renderer: everything but what no longer applies.
pub fn filter_response_headers<'a>(
    headers: impl IntoIterator<Item = (&'a str, &'a str)>,
) -> HeaderList {
    filter_headers(headers, &STRIPPED_RESPONSE_HEADERS)
}

/// What every response from the server is served with, forwarded or replayed
/// from the attachment store. An attachment keeps the uploader's type and
/// `inline`, on the renderer's own origin: framed (the PDF preview frames any
/// type containing `pdf`), an HTML one would run same-origin and reach the
/// bridge. `sandbox` gives such a document an opaque origin and no scripts, and
/// `nosniff` stops a type being guessed from the bytes. They replace any the
/// server sent.
pub const PROXIED_RESPONSE_GUARDS: [(&str, &str); 2] = [
    ("x-content-type-options", "nosniff"),
    ("content-security-policy", "sandbox; default-src 'none'"),
];

fn guarded(mut headers: HeaderList) -> HeaderList {
    headers.retain(|(name, _)| {
        PROXIED_RESPONSE_GUARDS
            .iter()
            .all(|(guard, _)| guard != name)
    });
    for (name, value) in PROXIED_RESPONSE_GUARDS {
        headers.push((name.to_string(), value.to_string()));
    }
    headers
}

/// The headers a response forwarded from the server is served with: filtered,
/// then guarded.
pub fn proxied_response_headers<'a>(
    headers: impl IntoIterator<Item = (&'a str, &'a str)>,
) -> HeaderList {
    guarded(filter_response_headers(headers))
}

/// True when a path would climb out of its directory, or cannot be trusted not
/// to: a `.` or `..` segment, a control character (a URL parser strips tab and
/// newline, which can complete a `..`), or a NUL.
///
/// Checked raw and after each percent-decoding, with `\` a separator at every
/// level (a WHATWG parser reads it as `/` for `http(s)`, and Windows does for
/// files). `%2e%2e`, `..%5c` and `%252e%252e` are all caught. A malformed
/// escape in the path as given is refused: one bad escape would otherwise stop
/// the decoding and hide a `%2e%2e` elsewhere in the path, which the URL parser
/// still collapses. A later level may fail (`%25` decodes to a literal `%`, and
/// nothing downstream decodes that again), so only the first level refuses. (The
/// JS lets it through; this is a deliberate divergence, and harmless because the
/// frontend builds paths with `encodeURIComponent` and never sends one.)
///
/// A segment is a dot segment when it is any WHATWG dot form, ASCII
/// case-insensitively: `.`, `%2e`, `..`, `.%2e`, `%2e.`, `%2e%2e`.
/// Each decoding shortens the path, so the loop ends.
pub fn traverses(pathname: &str) -> bool {
    traverses_with(pathname, true)
}

/// [`traverses`], where `refuse_malformed` says whether a malformed escape in
/// the path as given is refused. A path that was already decoded once may hold
/// a literal `%` (`100%.js`), which is not an escape and must not be.
fn traverses_with(pathname: &str, refuse_malformed: bool) -> bool {
    let mut current = pathname.to_string();
    let mut first = refuse_malformed;
    loop {
        if current.chars().any(char::is_control) || current.split(['/', '\\']).any(is_dot_segment) {
            return true;
        }
        match decode_uri_component(&current) {
            None => return first,
            Some(next) if next != current => current = next,
            Some(_) => return false,
        }
        first = false;
    }
}

fn is_dot_segment(segment: &str) -> bool {
    const FORMS: [&str; 6] = [".", "%2e", "..", ".%2e", "%2e.", "%2e%2e"];
    FORMS.iter().any(|form| segment.eq_ignore_ascii_case(form))
}

/// True when a path should be forwarded to the AgentRQ server rather than
/// served from the bundled renderer assets.
pub fn is_proxy_path(pathname: &str) -> bool {
    PROXY_PREFIXES.iter().any(|prefix| {
        if prefix.ends_with('/') {
            pathname.starts_with(prefix)
        } else {
            pathname == *prefix || pathname.starts_with(prefix)
        }
    })
}

/// Whether a request with this method carries a body that must be streamed on.
/// `GET` and `HEAD` never do, whatever the request object says.
pub fn sends_body(method: &str, has_body: bool) -> bool {
    has_body && !method.eq_ignore_ascii_case("GET") && !method.eq_ignore_ascii_case("HEAD")
}

/// Where a path is answered from.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Route {
    /// 403: the path [`traverses`], whatever it starts with.
    Forbidden,
    /// The extension drawer's document, matched exactly.
    DrawerFrame,
    /// A drawer's code, by format. `None` when the name's escapes are malformed.
    DrawerCode(Option<String>),
    /// Forwarded to the AgentRQ server.
    Proxy,
    /// Forwarded to the dev server, which has its own SPA fallback.
    DevServer,
    /// Served from the bundled renderer.
    Static,
}

/// Decide where a path is answered from, in the order the JS handler asks.
pub fn route(pathname: &str, dev: bool) -> Route {
    // First, before any prefix is compared: what a prefix matches must be what
    // the path means, and `/api/../x` does not mean `/api/`.
    if traverses(pathname) {
        return Route::Forbidden;
    }
    if pathname == DRAWER_FRAME_PATH {
        return Route::DrawerFrame;
    }
    if let Some(rest) = pathname.strip_prefix(DRAWER_CODE_PREFIX) {
        let format = rest.strip_suffix(".js").unwrap_or(rest);
        return Route::DrawerCode(decode_uri_component(format));
    }
    if is_proxy_path(pathname) {
        return Route::Proxy;
    }
    if dev {
        return Route::DevServer;
    }
    Route::Static
}

/// What to serve for a non-proxied path.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StaticPlan {
    Index,
    File { path: String },
    NotFound,
}

/// Decide what to serve for a non-proxied path, mirroring the SPA fallback in
/// the backend: a real file wins; otherwise a path that looks like an asset
/// (an extension that is not `.html`) is a 404, and anything else falls back to
/// `index.html` so the router can handle the route.
pub fn plan_static(pathname: &str, exists: bool) -> StaticPlan {
    if pathname == "/" || pathname.is_empty() {
        return StaticPlan::Index;
    }
    if exists {
        return StaticPlan::File {
            path: pathname.to_string(),
        };
    }
    if let Some(dot) = pathname.rfind('.') {
        // A dot before the last slash belongs to a directory segment.
        let after_slash = pathname.rfind('/').is_none_or(|slash| dot > slash);
        if after_slash && &pathname[dot..] != ".html" {
            return StaticPlan::NotFound;
        }
    }
    StaticPlan::Index
}

/// Reject paths that must never reach the filesystem.
///
/// Takes the path *after* its one decoding (see [`decode_asset_path`]), so a `%`
/// in it is a literal one. Checked on the decoded path, because decoding is
/// what can reintroduce a dangerous character: `%00` becomes a NUL, `%2e%2e`
/// becomes `..`.
///
/// Beyond [`traverses`], every segment must be a plain name, because the path
/// ends up joined onto a directory on a platform whose rules this code does not
/// choose: no `\` (a separator on Windows, at any decoding level), no `:` (a
/// drive letter, `\\?\`, an alternate stream), and no empty segment (`//` is
/// a UNC prefix, and a leading one is absolute). Only a trailing `/` may be empty.
/// Windows also trims a trailing `.` or space from a name and opens a reserved
/// device name (`con`, `aux.js`) as the device, so neither is a file name.
pub fn is_safe_asset_path(pathname: &str) -> bool {
    if traverses_with(pathname, false) || pathname.contains(['\\', ':']) {
        return false;
    }
    if pathname.split('/').any(is_windows_unsafe_name) {
        return false;
    }
    let mut segments = pathname.split('/');
    // The leading `/` leaves one empty segment first.
    segments.next();
    let rest: Vec<&str> = segments.collect();
    match rest.split_last() {
        Some((_, inner)) => inner.iter().all(|segment| !segment.is_empty()),
        None => true,
    }
}

fn is_windows_unsafe_name(segment: &str) -> bool {
    if segment.ends_with(['.', ' ']) {
        return true;
    }
    let stem = segment.split('.').next().unwrap_or("");
    let stem = stem.trim_end_matches(' ');
    let device = ["CON", "PRN", "AUX", "NUL"]
        .iter()
        .any(|name| stem.eq_ignore_ascii_case(name))
        || ["COM", "LPT"].iter().any(|prefix| {
            stem.len() == 4
                && stem[..3].eq_ignore_ascii_case(prefix)
                && matches!(stem.as_bytes()[3], b'1'..=b'9')
        });
    device
}

/// A path is percent-encoded but the filesystem needs real characters; without
/// decoding, an asset whose name has a space would be looked up as `%20`.
///
/// `None` when an escape sequence is malformed.
pub fn decode_asset_path(pathname: &str) -> Option<String> {
    decode_uri_component(pathname)
}

/// A file to serve from the bundled renderer.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StaticServe {
    /// The path to read, `/`-rooted inside the renderer directory.
    pub file_path: String,
    /// True for the SPA entry, which carries the policy and is never cached.
    pub index: bool,
}

impl StaticServe {
    /// The path without its leading `/`, to join onto the renderer directory:
    /// `Path::join` with an absolute path *replaces* the base. The shell must
    /// still canonicalise the result and check it starts with the root before it
    /// reads anything.
    pub fn relative_path(&self) -> &str {
        self.file_path.trim_start_matches('/')
    }

    /// The response headers. `csp` is called only for the SPA entry, so the
    /// policy is built per request and never for an ordinary asset.
    pub fn headers(&self, csp: &dyn Fn() -> String) -> HeaderList {
        let mut headers = vec![(
            "content-type".to_string(),
            mime_type_for(&self.file_path).to_string(),
        )];
        if self.index {
            headers.push(("content-security-policy".to_string(), csp()));
            // The SPA entry: a stale copy would pin the app to an old asset
            // graph after an update.
            headers.push(("cache-control".to_string(), "no-store".to_string()));
        }
        headers
    }
}

/// The answer for a request served from the bundled renderer.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StaticOutcome {
    /// 403: the path is malformed or must not reach the disk.
    Forbidden,
    /// 404: an asset that does not exist.
    NotFound,
    Serve(StaticServe),
}

/// Decide the answer for a path (still percent-encoded) served from disk.
///
/// `exists` is asked once, with the decoded path, and only for a safe one. It
/// is passed in rather than probed here so the rule stays free of the disk.
pub fn plan_static_request(raw_pathname: &str, exists: &dyn Fn(&str) -> bool) -> StaticOutcome {
    let Some(pathname) = decode_asset_path(raw_pathname) else {
        return StaticOutcome::Forbidden;
    };
    if !is_safe_asset_path(&pathname) {
        return StaticOutcome::Forbidden;
    }
    match plan_static(&pathname, exists(&pathname)) {
        StaticPlan::NotFound => StaticOutcome::NotFound,
        StaticPlan::Index => StaticOutcome::Serve(StaticServe {
            file_path: "/index.html".to_string(),
            index: true,
        }),
        StaticPlan::File { path } => StaticOutcome::Serve(StaticServe {
            file_path: path,
            index: false,
        }),
    }
}

impl StaticOutcome {
    /// The file to serve, when there is one.
    pub fn serve(self) -> Option<StaticServe> {
        match self {
            Self::Serve(serve) => Some(serve),
            Self::Forbidden | Self::NotFound => None,
        }
    }
}

/// The WebSocket origin for a server, or `""` when there is not one yet.
///
/// The terminal socket is the only thing the renderer opens that does not go
/// through the protocol handler, so it is addressed absolutely at the configured
/// server, and `connect-src 'self'` refuses it unless it is listed. Only the
/// origin is returned, and only the `ws`/`wss` form of it: the server's https
/// origin does not belong in `connect-src`, because nothing in the renderer is
/// allowed to address it directly.
pub fn web_socket_origin(server_url: &str) -> String {
    if server_url.is_empty() {
        return String::new();
    }
    // Before the connection screen is answered this is whatever somebody has
    // typed so far. A policy with a broken entry is rejected wholesale.
    let Ok(url) = Url::parse(server_url) else {
        return String::new();
    };
    let scheme = match url.scheme() {
        "https" => "wss",
        "http" => "ws",
        _ => return String::new(),
    };
    // An http(s) URL always has a host, so there is nothing to refuse here.
    let host = url.host_str().unwrap_or_default();
    // `;` and `,` are valid in a host, and in a header they end a directive and
    // a policy. A host that is not a plain name or an IPv6 literal gets no
    // entry. (The JS lets it through; this is a deliberate divergence.)
    if !is_plain_host(host) {
        return String::new();
    }
    match url.port() {
        Some(port) => format!("{scheme}://{host}:{port}"),
        None => format!("{scheme}://{host}"),
    }
}

fn is_plain_host(host: &str) -> bool {
    let name = |c: char| c.is_ascii_alphanumeric() || c == '.' || c == '-';
    match host.strip_prefix('[').and_then(|h| h.strip_suffix(']')) {
        Some(literal) => {
            !literal.is_empty() && literal.chars().all(|c| c.is_ascii_hexdigit() || c == ':')
        }
        None => !host.is_empty() && host.chars().all(name),
    }
}

/// What the policy is built from.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct CspOptions {
    /// The Vite client needs inline and eval'd script, so the policy is relaxed
    /// in dev and there only.
    pub dev: bool,
    pub dev_server_url: String,
    /// For the terminal socket, and for nothing else.
    pub server_url: String,
}

/// Content-Security-Policy for the renderer.
///
/// `wasm-unsafe-eval` is what lets the speech-to-text worker instantiate its
/// WASM module, and `worker-src blob:` is how that worker is spawned. Model
/// weights come from the Hugging Face CDN, so those hosts are in `connect-src`;
/// API traffic needs nothing beyond `'self'` because it is proxied through this
/// same origin. `img-src` lists the sign-in providers' avatar CDNs host by
/// host rather than opening it to `https:`: an `<img>` to an attacker's URL is
/// a working beacon even though it renders nothing.
///
/// Deliberately absent: COOP/COEP, because `require-corp` would block every CDN
/// response that lacks a CORP header, including the model weights.
pub fn build_csp(options: &CspOptions) -> String {
    let hf = "https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co";
    // Google serves avatars from lh3/lh4/lh5.googleusercontent.com and rotates
    // between them; GitHub uses a single host. A new sign-in provider means a
    // new entry here.
    let avatars = "https://*.googleusercontent.com https://avatars.githubusercontent.com";
    let script = if options.dev {
        "'self' 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'"
    } else {
        "'self' 'wasm-unsafe-eval'"
    };
    // In dev the local sockets stay listed as well: they cover Vite's HMR
    // client, which is a different connection to a different port.
    let socket = web_socket_origin(&options.server_url);
    let connect = if options.dev {
        format!(
            "'self' {hf} {} ws://localhost:* ws://127.0.0.1:* {socket}",
            options.dev_server_url
        )
    } else {
        format!("'self' {hf} {socket}")
    };
    let connect = connect.trim_end();

    [
        "default-src 'self'".to_string(),
        format!("script-src {script}"),
        "style-src 'self' 'unsafe-inline'".to_string(),
        format!("img-src 'self' data: blob: {avatars}"),
        "font-src 'self' data:".to_string(),
        format!("connect-src {connect}"),
        "worker-src 'self' blob:".to_string(),
        "media-src 'self' blob:".to_string(),
        // No plugins, and no way for injected markup to navigate the shell away
        // from the app or embed it in a frame.
        "object-src 'none'".to_string(),
        "frame-ancestors 'none'".to_string(),
        "base-uri 'self'".to_string(),
        "form-action 'self'".to_string(),
    ]
    .join("; ")
}

/// The `content-type` for a path, from its extension.
pub fn mime_type_for(pathname: &str) -> &'static str {
    let Some(dot) = pathname.rfind('.') else {
        return "application/octet-stream";
    };
    if pathname.rfind('/').is_some_and(|slash| dot < slash) {
        return "application/octet-stream";
    }
    match pathname[dot..].to_lowercase().as_str() {
        ".html" => "text/html; charset=utf-8",
        ".js" | ".mjs" => "text/javascript; charset=utf-8",
        ".css" => "text/css; charset=utf-8",
        ".json" | ".map" => "application/json; charset=utf-8",
        ".svg" => "image/svg+xml",
        ".png" => "image/png",
        ".jpg" | ".jpeg" => "image/jpeg",
        ".gif" => "image/gif",
        ".webp" => "image/webp",
        ".ico" => "image/x-icon",
        ".woff" => "font/woff",
        ".woff2" => "font/woff2",
        ".ttf" => "font/ttf",
        ".wasm" => "application/wasm",
        ".txt" => "text/plain; charset=utf-8",
        _ => "application/octet-stream",
    }
}

/// A response the shell can send as it is.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Reply {
    pub status: u16,
    pub headers: HeaderList,
    pub body: String,
}

impl Reply {
    /// A plain-text reply, as `new Response("…", { status })` builds.
    pub fn text(status: u16, body: &str) -> Self {
        Self {
            status,
            headers: vec![(
                "content-type".to_string(),
                "text/plain;charset=UTF-8".to_string(),
            )],
            body: body.to_string(),
        }
    }

    /// A JSON reply from a body already serialised, as `Response.json` builds.
    fn json(status: u16, body: String) -> Self {
        Self {
            status,
            headers: vec![("content-type".to_string(), "application/json".to_string())],
            body,
        }
    }

    pub fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(key, _)| key == name)
            .map(|(_, value)| value.as_str())
    }
}

/// The 403 for a path that must not reach the disk.
pub fn forbidden_reply() -> Reply {
    Reply::text(403, "Forbidden")
}

/// The 404 for an asset that does not exist.
pub fn not_found_reply() -> Reply {
    Reply::text(404, "Not Found")
}

/// The drawer frame's own answer. The document and its policy come from the
/// extension code; both are served together and never cached, because the
/// document is generated and a stale bootstrap is the kind of thing nobody
/// thinks to look for.
pub fn drawer_frame_reply(html: &str, csp: &str) -> Reply {
    Reply {
        status: 200,
        headers: vec![
            (
                "content-type".to_string(),
                "text/html; charset=utf-8".to_string(),
            ),
            ("content-security-policy".to_string(), csp.to_string()),
            ("cache-control".to_string(), "no-store".to_string()),
        ],
        body: html.to_string(),
    }
}

/// What a drawer's 404 says when nothing supplies drawers at all.
pub const NO_DRAWER_SOURCE: &str = "Extensions are unavailable.";

/// What the extensions layer says about a drawer format.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DrawerLookup {
    Found { code: String },
    Refused { reason: Option<String> },
}

impl Default for DrawerLookup {
    /// No source of drawers: every format is refused, saying so.
    fn default() -> Self {
        Self::Refused {
            reason: Some(NO_DRAWER_SOURCE.to_string()),
        }
    }
}

/// The answer for a drawer's code.
pub fn drawer_code_reply(found: &DrawerLookup) -> Reply {
    match found {
        DrawerLookup::Refused { reason } => {
            Reply::text(404, reason.as_deref().unwrap_or("No such drawer"))
        }
        DrawerLookup::Found { code } => Reply {
            status: 200,
            headers: vec![
                (
                    "content-type".to_string(),
                    "text/javascript; charset=utf-8".to_string(),
                ),
                // The frame's origin is opaque, so an ES module import from it
                // is a cross-origin request that needs CORS to be readable.
                ("access-control-allow-origin".to_string(), "*".to_string()),
                // What is installed can change under the app, and a cached
                // drawer would outlive the extension that supplied it.
                ("cache-control".to_string(), "no-store".to_string()),
            ],
            body: code.clone(),
        },
    }
}

/// Why a request could not be forwarded.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProxyError {
    /// No server is configured, or the one that is cannot be a base URL.
    NoServer,
    /// The path [`traverses`]: it is refused rather than forwarded anywhere.
    Forbidden,
    /// No response headers arrived within the budget.
    Timeout,
    /// The body went silent for [`PROXY_IDLE_TIMEOUT_MS`].
    Stalled,
    /// The body is over [`MAX_PROXIED_BODY_BYTES`].
    TooLarge,
    /// The request failed; the message is the failure's own.
    Unreachable(String),
}

/// The reply for a request that could not be forwarded.
///
/// A timeout is its own answer: 502 says the server refused us, and the
/// renderer's retry policy for the two is not the same. The server being
/// unreachable is an ordinary state for a desktop client, so it is a normal
/// response to the renderer rather than a failed handler.
pub fn proxy_error_reply(error: &ProxyError) -> Reply {
    match error {
        ProxyError::Forbidden => forbidden_reply(),
        ProxyError::NoServer => Reply::json(
            503,
            r#"{"error":"No AgentRQ server is configured"}"#.to_string(),
        ),
        ProxyError::Timeout => Reply::json(
            504,
            r#"{"error":"The AgentRQ server did not respond in time"}"#.to_string(),
        ),
        ProxyError::Stalled => Reply::json(
            504,
            r#"{"error":"The AgentRQ server stopped sending its response"}"#.to_string(),
        ),
        ProxyError::TooLarge => Reply::json(
            502,
            r#"{"error":"The AgentRQ server's response is too large"}"#.to_string(),
        ),
        ProxyError::Unreachable(detail) => Reply::json(
            502,
            format!(
                r#"{{"error":"Cannot reach the AgentRQ server","detail":{}}}"#,
                json_string(detail)
            ),
        ),
    }
}

/// The reply when the dev server cannot be reached, which is 502 for a refusal
/// and for a deadline alike.
pub fn dev_unreachable_reply(message: &str) -> Reply {
    Reply::text(502, &format!("Cannot reach the dev server: {message}"))
}

/// The message a deadline carries, which a dev request surfaces in its 502.
pub fn timeout_message(ms: u64) -> String {
    format!("no response headers after {ms}ms")
}

/// What a dev request that failed surfaces in its 502.
pub fn dev_failure_message(error: &ProxyError) -> String {
    match error {
        ProxyError::Unreachable(detail) => detail.clone(),
        ProxyError::Stalled => format!("no data for {PROXY_IDLE_TIMEOUT_MS}ms"),
        ProxyError::TooLarge => format!("over {MAX_PROXIED_BODY_BYTES} bytes"),
        // A dev request has no server to miss and no path to refuse.
        _ => timeout_message(PROXY_TIMEOUT_MS),
    }
}

fn json_string(value: &str) -> String {
    serde_json::Value::String(value.to_string()).to_string()
}

/// The response headers for a page from the dev server: the same filtering, and
/// the app's policy on HTML only.
pub fn dev_response_headers<'a>(
    upstream: impl IntoIterator<Item = (&'a str, &'a str)>,
    csp: &dyn Fn() -> String,
) -> HeaderList {
    let mut headers = filter_response_headers(upstream);
    let is_html = headers
        .iter()
        .any(|(name, value)| name == "content-type" && value.contains("text/html"));
    if is_html {
        headers.retain(|(name, _)| name != "content-security-policy");
        headers.push(("content-security-policy".to_string(), csp()));
    }
    headers
}

/// Whether a proxied request goes through the attachment store: a `GET` for
/// `…/tasks/:id/attachments/:id`, and only under a proxied prefix, as the JS
/// reaches the store only inside its proxy branch. Anything else is forwarded
/// untouched.
pub fn serve_from_store(method: &str, pathname: &str) -> bool {
    method == "GET" && is_proxy_path(pathname) && !traverses(pathname) && {
        let segments: Vec<&str> = pathname.split('/').collect();
        matches!(
            segments.as_slice(),
            [_, .., "tasks", task, "attachments", attachment]
                if !task.is_empty() && !attachment.is_empty()
        )
    }
}

/// A response whose body is bytes.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BytesReply {
    pub status: u16,
    pub headers: HeaderList,
    pub body: Vec<u8>,
}

/// The answer for an attachment found in the store: `content-type` always,
/// `content-disposition` only when there is one, and the guards a forwarded
/// response gets.
pub fn attachment_hit_reply(
    body: Vec<u8>,
    content_type: &str,
    content_disposition: &str,
) -> BytesReply {
    let mut headers = vec![("content-type".to_string(), content_type.to_string())];
    if !content_disposition.is_empty() {
        headers.push((
            "content-disposition".to_string(),
            content_disposition.to_string(),
        ));
    }
    BytesReply {
        status: 200,
        headers: guarded(headers),
        body,
    }
}

/// The 502 for an attachment whose body could not be read to the end.
pub fn bad_gateway_reply() -> Reply {
    Reply::text(502, "Bad Gateway")
}

/// What an attachment is stored with, from the upstream header: `""` when the
/// response had none.
pub fn stored_content_disposition(upstream: Option<&str>) -> &str {
    upstream.unwrap_or("")
}

/// The two budgets for a forwarded request.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Timeouts {
    pub proxy_ms: u64,
    pub upload_ms: u64,
}

impl Default for Timeouts {
    fn default() -> Self {
        Self {
            proxy_ms: PROXY_TIMEOUT_MS,
            upload_ms: UPLOAD_TIMEOUT_MS,
        }
    }
}

impl Timeouts {
    /// A request carrying a body gets the longer one: the server cannot send
    /// headers until it has read the upload, so the transfer is inside the
    /// window.
    pub fn for_request(&self, has_body: bool) -> u64 {
        if has_body {
            self.upload_ms
        } else {
            self.proxy_ms
        }
    }
}

/// The URL a path is forwarded to: the path and query on the server's origin.
/// `search` carries its `?`.
///
/// Built from the origin and the path, never by resolving the path against the
/// base, so nothing in it can change the host (`//host/x`) or be collapsed
/// after the fact. A path that [`traverses`] has no URL.
///
/// After the parser has run, the path must still have the shape it was given: a
/// parser that collapsed or split a segment is refused, so no gap in
/// [`traverses`] becomes an escape from the allow-list. `allowed` is that
/// allow-list, tested on the parsed path.
fn join_target(
    base: &str,
    pathname: &str,
    search: &str,
    allowed: &dyn Fn(&str) -> bool,
) -> Result<String, ProxyError> {
    if traverses(pathname) {
        return Err(ProxyError::Forbidden);
    }
    let invalid = || ProxyError::Unreachable("invalid server URL".to_string());
    let mut url = Url::parse(base).map_err(|_| invalid())?;
    if url.cannot_be_a_base() {
        return Err(invalid());
    }
    url.set_path(pathname);
    let segments = |path: &str| path.split(['/', '\\']).count();
    let given = if pathname.starts_with('/') {
        segments(pathname)
    } else {
        segments(pathname) + 1
    };
    if segments(url.path()) != given || !allowed(url.path()) {
        return Err(ProxyError::Forbidden);
    }
    url.set_query(search.strip_prefix('?'));
    url.set_fragment(None);
    Ok(url.into())
}

fn proxy_url(base: &str, pathname: &str, search: &str) -> Result<String, ProxyError> {
    if base.is_empty() {
        // Before the first run's connection screen is answered there is nowhere
        // to forward to. Saying so plainly beats failing the load.
        return Err(ProxyError::NoServer);
    }
    if !is_proxy_path(pathname) {
        return Err(ProxyError::Forbidden);
    }
    join_target(base, pathname, search, &is_proxy_path)
}

/// What the shell reports and the protocol is configured with.
///
/// The server is a function rather than a value: it is answered by the shell
/// and changes under a running app (switching profile, answering the connection
/// screen), and the policy names it, so a policy built once at startup would
/// still name the previous server.
pub struct AppProtocol<S: Fn() -> String> {
    server_url: S,
    dev_server_url: String,
    timeouts: Timeouts,
}

impl<S: Fn() -> String> AppProtocol<S> {
    pub fn new(server_url: S, dev_server_url: &str) -> Self {
        Self {
            server_url,
            dev_server_url: dev_server_url.to_string(),
            timeouts: Timeouts::default(),
        }
    }

    pub fn with_timeouts(mut self, timeouts: Timeouts) -> Self {
        self.timeouts = timeouts;
        self
    }

    /// Whether static assets come from the dev server instead of disk.
    pub fn dev(&self) -> bool {
        !self.dev_server_url.is_empty()
    }

    /// Where a path is answered from.
    pub fn route(&self, pathname: &str) -> Route {
        route(pathname, self.dev())
    }

    /// The policy, rebuilt on every call so it names the server as it is now.
    pub fn csp(&self) -> String {
        build_csp(&CspOptions {
            dev: self.dev(),
            dev_server_url: self.dev_server_url.clone(),
            server_url: (self.server_url)(),
        })
    }

    /// The URL to forward a request to, or why there is none.
    pub fn proxy_target(&self, pathname: &str, search: &str) -> Result<String, ProxyError> {
        proxy_url(&(self.server_url)(), pathname, search)
    }

    /// The URL to fetch from the dev server, or the message for the 502.
    pub fn dev_target(&self, pathname: &str, search: &str) -> Result<String, String> {
        join_target(&self.dev_server_url, pathname, search, &|_| true)
            .map_err(|_| "Invalid URL".to_string())
    }

    /// How long a forwarded request may take to produce response headers.
    pub fn timeout_ms(&self, has_body: bool) -> u64 {
        self.timeouts.for_request(has_body)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::{Cell, RefCell};

    const SERVER: &str = "http://localhost:3000";

    fn proto(server: &str) -> AppProtocol<impl Fn() -> String> {
        let server = server.to_string();
        AppProtocol::new(move || server.clone(), "")
    }

    fn dev_proto() -> AppProtocol<impl Fn() -> String> {
        dev_proto_at("http://localhost:5174")
    }

    fn dev_proto_at(dev_server_url: &str) -> AppProtocol<impl Fn() -> String> {
        AppProtocol::new(|| SERVER.to_string(), dev_server_url)
    }

    fn plan_with(probe: &Probe, raw: &str) -> StaticOutcome {
        plan_static_request(raw, &|path| probe.exists(path))
    }

    fn headers_counted(serve: &StaticServe, calls: &Cell<u32>) -> HeaderList {
        serve.headers(&|| counted(calls))
    }

    fn dev_headers_counted(
        upstream: Vec<(&'static str, &'static str)>,
        calls: &Cell<u32>,
    ) -> HeaderList {
        dev_response_headers(upstream, &|| counted(calls))
    }

    fn opts(dev: bool, dev_server_url: &str, server_url: &str) -> CspOptions {
        CspOptions {
            dev,
            dev_server_url: dev_server_url.to_string(),
            server_url: server_url.to_string(),
        }
    }

    fn directive(csp: &str, name: &str) -> String {
        csp.split("; ")
            .find(|d| d.starts_with(name))
            .unwrap()
            .to_string()
    }

    /// A filesystem that says everything exists and remembers what it was asked.
    #[derive(Default)]
    struct Probe {
        asked: Cell<u32>,
        paths: RefCell<Vec<String>>,
    }

    impl Probe {
        fn exists(&self, path: &str) -> bool {
            self.asked.set(self.asked.get() + 1);
            self.paths.borrow_mut().push(path.to_string());
            true
        }
    }

    /// A policy builder that counts how often it was asked.
    fn counted(calls: &Cell<u32>) -> String {
        calls.set(calls.get() + 1);
        "policy".to_string()
    }

    fn pairs(list: &[(&'static str, &'static str)]) -> Vec<(&'static str, &'static str)> {
        list.to_vec()
    }

    fn names(headers: &HeaderList) -> Vec<&str> {
        headers.iter().map(|(name, _)| name.as_str()).collect()
    }

    fn get<'a>(headers: &'a HeaderList, name: &str) -> Option<&'a str> {
        headers
            .iter()
            .find(|(key, _)| key == name)
            .map(|(_, value)| value.as_str())
    }

    // isProxyPath

    #[test]
    fn forwards_the_api_mcp_and_well_known_prefixes() {
        assert!(is_proxy_path("/api/v1/workspaces"));
        assert!(is_proxy_path("/.well-known/oauth-authorization-server"));
    }

    #[test]
    fn matches_mcp_both_bare_and_with_a_workspace_segment() {
        // The backend registers /mcp without a trailing slash, so both forms route.
        assert!(is_proxy_path("/mcp"));
        assert!(is_proxy_path("/mcp/0ZzhYQG2qtl"));
    }

    #[test]
    fn leaves_renderer_routes_alone() {
        assert!(!is_proxy_path("/"));
        assert!(!is_proxy_path("/workspaces/abc/board"));
        assert!(!is_proxy_path("/assets/index-abc123.js"));
    }

    #[test]
    fn does_not_forward_a_route_that_merely_starts_with_the_word_api() {
        // '/api/' carries its slash precisely so this stays a renderer route.
        assert!(!is_proxy_path("/apiary"));
    }

    #[test]
    fn exposes_the_prefixes_it_uses() {
        assert_eq!(PROXY_PREFIXES, ["/api/", "/mcp", "/.well-known/"]);
    }

    // planStatic

    #[test]
    fn serves_index_html_at_the_root() {
        assert_eq!(plan_static("/", false), StaticPlan::Index);
        assert_eq!(plan_static("", false), StaticPlan::Index);
    }

    #[test]
    fn serves_a_file_that_exists() {
        assert_eq!(
            plan_static("/assets/app.js", true),
            StaticPlan::File {
                path: "/assets/app.js".to_string()
            }
        );
    }

    #[test]
    fn four_oh_fours_a_missing_asset_rather_than_handing_back_the_spa() {
        // Returning index.html for a missing .js would make the browser parse
        // HTML as a script.
        assert_eq!(
            plan_static("/assets/missing.js", false),
            StaticPlan::NotFound
        );
        assert_eq!(plan_static("/favicon.ico", false), StaticPlan::NotFound);
    }

    #[test]
    fn falls_back_to_index_html_for_spa_routes() {
        assert_eq!(
            plan_static("/workspaces/abc/board", false),
            StaticPlan::Index
        );
        assert_eq!(plan_static("/tasks/all", false), StaticPlan::Index);
    }

    #[test]
    fn treats_html_as_a_route_not_an_asset() {
        assert_eq!(plan_static("/anything.html", false), StaticPlan::Index);
    }

    #[test]
    fn ignores_a_dot_that_belongs_to_a_directory_segment() {
        // The dot is before the last slash, so this has no extension.
        assert_eq!(plan_static("/v1.2/board", false), StaticPlan::Index);
    }

    #[test]
    fn a_path_with_no_slash_is_judged_by_its_dot() {
        assert_eq!(plan_static("app.js", false), StaticPlan::NotFound);
        assert_eq!(plan_static("route", false), StaticPlan::Index);
    }

    // isSafeAssetPath

    #[test]
    fn accepts_ordinary_asset_paths() {
        assert!(is_safe_asset_path("/assets/index.js"));
        assert!(is_safe_asset_path("/a..b/c"));
    }

    #[test]
    fn rejects_traversal_and_null_bytes() {
        assert!(!is_safe_asset_path("/../../etc/passwd"));
        assert!(!is_safe_asset_path("/assets/\0.js"));
    }

    #[test]
    fn exports_decode_asset_path_which_reports_malformed_escapes_as_none() {
        assert_eq!(
            decode_asset_path("/assets/my%20file.js").as_deref(),
            Some("/assets/my file.js")
        );
        assert_eq!(decode_asset_path("/assets/%zz.js"), None);
    }

    // filterRequestHeaders

    #[test]
    fn drops_headers_that_describe_the_app_origin() {
        let out = filter_request_headers(pairs(&[
            ("origin", "app://agentrq"),
            ("referer", "app://agentrq/login"),
            ("host", "agentrq"),
            ("content-length", "12"),
            ("content-type", "application/json"),
            ("accept", "text/event-stream"),
        ]));
        assert_eq!(get(&out, "origin"), None);
        assert_eq!(get(&out, "referer"), None);
        assert_eq!(get(&out, "host"), None);
        assert_eq!(get(&out, "content-length"), None);
        assert_eq!(get(&out, "content-type"), Some("application/json"));
        assert_eq!(get(&out, "accept"), Some("text/event-stream"));
    }

    #[test]
    fn matches_header_names_in_any_case_and_keeps_order_and_repeats() {
        let out = filter_request_headers(pairs(&[
            ("Origin", "app://agentrq"),
            ("X-One", "1"),
            ("x-one", "2"),
            ("HOST", "agentrq"),
        ]));
        assert_eq!(
            out,
            [
                ("x-one".to_string(), "1".to_string()),
                ("x-one".to_string(), "2".to_string())
            ]
        );
    }

    // filterResponseHeaders

    #[test]
    fn withholds_set_cookie_from_the_renderer() {
        // The cookie belongs to the main-process jar; the renderer must not fork it.
        let out = filter_response_headers(pairs(&[("set-cookie", "at=secret; HttpOnly")]));
        assert_eq!(get(&out, "set-cookie"), None);
    }

    #[test]
    fn drops_encoding_and_length_which_no_longer_describe_the_body() {
        let out = filter_response_headers(pairs(&[
            ("content-encoding", "gzip"),
            ("content-length", "4096"),
            ("content-type", "application/json"),
        ]));
        assert_eq!(get(&out, "content-encoding"), None);
        assert_eq!(get(&out, "content-length"), None);
        assert_eq!(get(&out, "content-type"), Some("application/json"));
    }

    #[test]
    fn drops_cors_headers_that_are_meaningless_once_same_origin() {
        let out = filter_response_headers(pairs(&[
            ("access-control-allow-origin", "*"),
            ("access-control-allow-credentials", "true"),
            ("access-control-allow-headers", "Origin"),
            ("access-control-allow-methods", "GET"),
            ("access-control-expose-headers", "mcp-session-id"),
            ("cache-control", "no-cache"),
        ]));
        assert_eq!(names(&out), ["cache-control"]);
    }

    #[test]
    fn drops_hop_by_hop_headers_both_ways() {
        let hop = [
            ("Connection", "keep-alive, X-Conn-Only"),
            ("keep-alive", "timeout=5"),
            ("te", "trailers"),
            ("trailer", "x-sum"),
            ("transfer-encoding", "chunked"),
            ("upgrade", "websocket"),
            ("proxy-authorization", "Basic x"),
            ("Proxy-Authenticate", "Basic"),
            ("x-conn-only", "1"),
            ("x-kept", "1"),
        ];
        assert_eq!(names(&filter_request_headers(pairs(&hop))), ["x-kept"]);
        assert_eq!(names(&filter_response_headers(pairs(&hop))), ["x-kept"]);
    }

    // proxiedResponseHeaders

    #[test]
    fn sandboxes_every_forwarded_response_in_place_of_the_servers_own_policy() {
        let out = proxied_response_headers(pairs(&[
            ("content-type", "text/html;pdf"),
            ("content-security-policy", "default-src *"),
            ("X-Content-Type-Options", "sniff"),
            ("set-cookie", "at=1"),
        ]));
        assert_eq!(
            out,
            [
                ("content-type".to_string(), "text/html;pdf".to_string()),
                ("x-content-type-options".to_string(), "nosniff".to_string()),
                (
                    "content-security-policy".to_string(),
                    "sandbox; default-src 'none'".to_string()
                ),
            ]
        );
    }

    // buildCSP

    #[test]
    fn allows_wasm_and_blob_workers_for_the_speech_to_text_pipeline() {
        let csp = build_csp(&CspOptions::default());
        assert!(csp.contains("script-src 'self' 'wasm-unsafe-eval'"));
        assert!(csp.contains("worker-src 'self' blob:"));
        assert!(csp.contains("huggingface.co"));
    }

    #[test]
    fn locks_down_navigation_and_embedding() {
        let csp = build_csp(&CspOptions::default());
        assert!(csp.contains("object-src 'none'"));
        assert!(csp.contains("frame-ancestors 'none'"));
        assert!(csp.contains("base-uri 'self'"));
        assert!(csp.contains("form-action 'self'"));
    }

    #[test]
    fn does_not_permit_eval_or_inline_script_in_production() {
        let csp = build_csp(&opts(false, "", ""));
        assert!(!csp.contains("'unsafe-eval'"));
        assert!(!csp.contains("script-src 'self' 'unsafe-inline'"));
    }

    #[test]
    fn allows_the_sign_in_providers_to_serve_profile_photos() {
        // user.picture is the provider's own URL, so 'self' alone blocked every
        // avatar and the sidebar fell back to the initial-letter placeholder.
        for csp in [
            build_csp(&CspOptions::default()),
            build_csp(&opts(true, "http://localhost:5174", "")),
        ] {
            let img = directive(&csp, "img-src");
            assert!(img.contains("https://*.googleusercontent.com"));
            assert!(img.contains("https://avatars.githubusercontent.com"));
            assert!(img.contains("'self'"));
            assert!(img.contains("data:"));
            assert!(img.contains("blob:"));
        }
    }

    #[test]
    fn does_not_open_images_to_arbitrary_origins() {
        // An <img> to any host is a working beacon even though it renders
        // nothing, so the avatar hosts are listed individually.
        let img = directive(&build_csp(&CspOptions::default()), "img-src");
        let sources: Vec<&str> = img.split(' ').collect();
        assert!(!sources.contains(&"https:"));
        assert!(!sources.contains(&"*"));
    }

    #[test]
    fn relaxes_only_in_dev_where_the_vite_client_needs_it() {
        let csp = build_csp(&opts(true, "http://localhost:5174", ""));
        assert!(csp.contains("'unsafe-eval'"));
        assert!(csp.contains("http://localhost:5174"));
        assert!(csp.contains("ws://localhost:*"));
    }

    #[test]
    fn allows_a_websocket_to_the_configured_server() {
        let connect = directive(
            &build_csp(&opts(false, "", "https://agentrq.example")),
            "connect-src",
        );
        assert!(connect.contains("wss://agentrq.example"));
    }

    #[test]
    fn allows_one_in_dev_too_without_dropping_the_vite_client() {
        let connect = directive(
            &build_csp(&opts(
                true,
                "http://localhost:5174",
                "http://localhost:3000",
            )),
            "connect-src",
        );
        assert!(connect.contains("ws://localhost:3000"));
        assert!(connect.contains("ws://localhost:*"));
        assert!(connect.contains("http://localhost:5174"));
    }

    #[test]
    fn does_not_also_allow_ordinary_requests_to_the_server() {
        let connect = directive(
            &build_csp(&opts(false, "", "https://agentrq.example")),
            "connect-src",
        );
        assert!(!connect.contains("https://agentrq.example"));
    }

    #[test]
    fn is_still_a_well_formed_policy_with_no_server_configured() {
        for server_url in ["", "not a url", "ftp://nope/"] {
            let csp = build_csp(&opts(false, "", server_url));
            assert!(csp.contains("connect-src 'self'"));
            // A trailing or doubled separator is what a naive concatenation
            // leaves behind, and Chromium discards a directive it cannot parse.
            assert!(!csp.contains(";;") && !csp.contains("; ;"));
            assert!(!csp.trim_end().ends_with(';'));
            assert!(!csp.contains("  "));
        }
    }

    /// Strings produced by running the JS `buildCSP` on the same options, so
    /// "identical" is checked byte for byte rather than by sampling directives.
    /// The empty `devServerUrl` in dev leaves a double space, as the JS does.
    #[test]
    fn is_byte_identical_to_the_js_policy() {
        let golden = [
        (
            opts(false, "", ""),
            "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        (
            opts(false, "", "https://agentrq.example"),
            "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co wss://agentrq.example; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        (
            opts(false, "", "http://localhost:3000"),
            "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co ws://localhost:3000; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        (
            opts(true, "http://localhost:5174", ""),
            "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co http://localhost:5174 ws://localhost:* ws://127.0.0.1:*; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        (
            opts(true, "http://localhost:5174", "http://localhost:3000"),
            "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co http://localhost:5174 ws://localhost:* ws://127.0.0.1:* ws://localhost:3000; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        (
            opts(true, "", ""),
            "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co  ws://localhost:* ws://127.0.0.1:*; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        (
            opts(true, "", "https://a.example:8443/x"),
            "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co  ws://localhost:* ws://127.0.0.1:* wss://a.example:8443; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        (
            opts(false, "", "ftp://nope/"),
            "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.googleusercontent.com https://avatars.githubusercontent.com; font-src 'self' data:; connect-src 'self' https://huggingface.co https://*.hf.co https://cdn-lfs.huggingface.co https://cdn-lfs-us-1.huggingface.co; worker-src 'self' blob:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
        ),
        ];
        for (options, expected) in golden {
            assert_eq!(build_csp(&options), expected, "{options:?}");
        }
    }

    // webSocketOrigin

    #[test]
    fn is_the_ws_form_of_the_server_origin_and_nothing_else_from_the_url() {
        assert_eq!(
            web_socket_origin("https://agentrq.example/base/path?q=1"),
            "wss://agentrq.example"
        );
        assert_eq!(
            web_socket_origin("http://localhost:3000/"),
            "ws://localhost:3000"
        );
        // The port is part of the origin, and dropping it addresses a different
        // server that is usually not there at all.
        assert_eq!(
            web_socket_origin("https://agentrq.example:8443"),
            "wss://agentrq.example:8443"
        );
    }

    #[test]
    fn answers_nothing_for_anything_that_is_not_a_server_address() {
        // Half-typed input from the connection screen reaches this, and a broken
        // source makes Chromium reject the whole directive.
        for bad in [
            "",
            "not a url",
            "ftp://files/",
            "app://bundle/index.html",
            "https://",
            "https://exa mple.com",
        ] {
            assert_eq!(web_socket_origin(bad), "", "{bad}");
        }
    }

    #[test]
    fn normalises_the_host_as_the_js_url_does() {
        // Checked against `new URL(...).host` in Node.
        assert_eq!(web_socket_origin("https://a.com:443"), "wss://a.com");
        assert_eq!(web_socket_origin("http://a.com:80/x"), "ws://a.com");
        assert_eq!(web_socket_origin("https://[::1]:8443/"), "wss://[::1]:8443");
        assert_eq!(web_socket_origin("HTTPS://Ex.COM"), "wss://ex.com");
        assert_eq!(web_socket_origin("https://user:pw@h.com/"), "wss://h.com");
        assert_eq!(
            web_socket_origin("http://xn--e1afmkfd.com"),
            "ws://xn--e1afmkfd.com"
        );
        assert_eq!(web_socket_origin("https:///x"), "wss://x");
    }

    // mimeTypeFor

    #[test]
    fn maps_the_types_the_renderer_bundle_actually_emits() {
        assert_eq!(mime_type_for("/index.html"), "text/html; charset=utf-8");
        assert_eq!(
            mime_type_for("/assets/app.js"),
            "text/javascript; charset=utf-8"
        );
        assert_eq!(
            mime_type_for("/assets/app.mjs"),
            "text/javascript; charset=utf-8"
        );
        assert_eq!(mime_type_for("/assets/app.css"), "text/css; charset=utf-8");
        assert_eq!(mime_type_for("/model.wasm"), "application/wasm");
        assert_eq!(mime_type_for("/inter.woff2"), "font/woff2");
        assert_eq!(mime_type_for("/FAVICON.SVG"), "image/svg+xml");
    }

    #[test]
    fn falls_back_to_octet_stream_when_there_is_no_usable_extension() {
        assert_eq!(mime_type_for("/noextension"), "application/octet-stream");
        assert_eq!(mime_type_for("/v1.2/board"), "application/octet-stream");
        assert_eq!(mime_type_for("/archive.zip"), "application/octet-stream");
    }

    #[test]
    fn maps_every_extension_the_js_table_lists() {
        for (path, mime) in [
            ("/a.json", "application/json; charset=utf-8"),
            ("/a.map", "application/json; charset=utf-8"),
            ("/a.png", "image/png"),
            ("/a.jpg", "image/jpeg"),
            ("/a.jpeg", "image/jpeg"),
            ("/a.gif", "image/gif"),
            ("/a.webp", "image/webp"),
            ("/a.ico", "image/x-icon"),
            ("/a.woff", "font/woff"),
            ("/a.ttf", "font/ttf"),
            ("/a.txt", "text/plain; charset=utf-8"),
        ] {
            assert_eq!(mime_type_for(path), mime, "{path}");
        }
        assert_eq!(mime_type_for("noslash.css"), "text/css; charset=utf-8");
    }

    // the served policy

    /// The index page's policy, with whatever server the shell currently reports.
    fn index_policy(protocol: &AppProtocol<impl Fn() -> String>) -> String {
        let serve = plan_static_request("/", &|_| false)
            .serve()
            .expect("served");
        get(
            &serve.headers(&|| protocol.csp()),
            "content-security-policy",
        )
        .unwrap()
        .to_string()
    }

    #[test]
    fn names_the_configured_server_so_the_terminal_socket_can_be_opened() {
        assert!(index_policy(&proto("https://agentrq.example")).contains("wss://agentrq.example"));
    }

    // Built per request rather than at startup, because the server is not fixed:
    // switching profile or answering the connection screen changes it, and a
    // policy captured once would still name the previous server.
    #[test]
    fn follows_the_server_changing_under_a_running_app() {
        let server = RefCell::new("https://first.example".to_string());
        let protocol = AppProtocol::new(|| server.borrow().clone(), "");

        assert!(index_policy(&protocol).contains("wss://first.example"));

        *server.borrow_mut() = "https://second.example".to_string();
        let after = index_policy(&protocol);
        assert!(after.contains("wss://second.example"));
        assert!(!after.contains("first.example"));
    }

    #[test]
    fn serves_a_valid_policy_before_any_server_is_configured() {
        let csp = index_policy(&proto(""));
        assert!(csp.contains("connect-src 'self'"));
        assert!(!csp.contains(";;") && !csp.contains("; ;"));
    }

    // proxying

    #[test]
    fn forwards_an_api_request_to_the_configured_server_preserving_the_query() {
        assert_eq!(
            proto(SERVER).proxy_target("/api/v1/tasks", "?limit=10&offset=20"),
            Ok("http://localhost:3000/api/v1/tasks?limit=10&offset=20".to_string())
        );
        assert_eq!(
            proto(SERVER).proxy_target("/api/v1/auth/user", ""),
            Ok("http://localhost:3000/api/v1/auth/user".to_string())
        );
    }

    #[test]
    fn resolves_against_the_server_origin_whatever_path_it_was_saved_with() {
        // `new URL("/api/…", "https://h.example/base/")` replaces the path.
        assert_eq!(
            proto("https://h.example/base/").proxy_target("/api/v1/tasks", ""),
            Ok("https://h.example/api/v1/tasks".to_string())
        );
    }

    #[test]
    fn sends_no_body_for_get_and_head() {
        assert!(!sends_body("GET", true));
        assert!(!sends_body("head", true));
        assert!(!sends_body("Head", true));
    }

    #[test]
    fn sends_a_body_for_any_other_method_that_has_one() {
        assert!(sends_body("POST", true));
        assert!(sends_body("put", true));
        assert!(sends_body("DELETE", true));
    }

    #[test]
    fn omits_the_body_when_a_post_carries_none() {
        assert!(!sends_body("POST", false));
    }

    #[test]
    fn reports_503_when_no_server_has_been_configured_yet() {
        // First run, before the connection screen is answered: there is nowhere
        // to forward to, and failing the load would surface as an opaque error.
        let error = proto("").proxy_target("/api/v1/auth/user", "").unwrap_err();
        assert_eq!(error, ProxyError::NoServer);

        let reply = proxy_error_reply(&error);
        assert_eq!(reply.status, 503);
        assert_eq!(reply.body, r#"{"error":"No AgentRQ server is configured"}"#);
        assert_eq!(reply.header("content-type"), Some("application/json"));
    }

    #[test]
    fn reports_an_unreachable_server_as_502_instead_of_failing() {
        let reply = proxy_error_reply(&ProxyError::Unreachable("ECONNREFUSED".to_string()));
        assert_eq!(reply.status, 502);
        assert_eq!(
            reply.body,
            r#"{"error":"Cannot reach the AgentRQ server","detail":"ECONNREFUSED"}"#
        );
    }

    #[test]
    fn carries_whatever_the_failure_said_as_the_detail() {
        // The JS survives a thrown string that is not an Error; here the detail
        // is a message and is escaped as JSON.
        let reply = proxy_error_reply(&ProxyError::Unreachable("socket \"hang\" up\n".to_string()));
        let parsed: serde_json::Value = serde_json::from_str(&reply.body).unwrap();
        assert_eq!(parsed["detail"], "socket \"hang\" up\n");
        assert_eq!(parsed["error"], "Cannot reach the AgentRQ server");
    }

    #[test]
    fn answers_504_rather_than_hanging_when_the_response_never_arrives() {
        let reply = proxy_error_reply(&ProxyError::Timeout);
        assert_eq!(reply.status, 504);
        assert_eq!(
            reply.body,
            r#"{"error":"The AgentRQ server did not respond in time"}"#
        );
    }

    #[test]
    fn answers_504_for_a_body_that_stalls_and_502_for_one_too_large() {
        let stalled = proxy_error_reply(&ProxyError::Stalled);
        assert_eq!(stalled.status, 504);
        assert_eq!(
            stalled.body,
            r#"{"error":"The AgentRQ server stopped sending its response"}"#
        );
        let large = proxy_error_reply(&ProxyError::TooLarge);
        assert_eq!(large.status, 502);
        assert_eq!(
            large.body,
            r#"{"error":"The AgentRQ server's response is too large"}"#
        );
        assert_eq!(PROXY_IDLE_TIMEOUT_MS, 30_000);
        assert_eq!(MAX_PROXIED_BODY_BYTES, 128 * 1024 * 1024);
    }

    #[test]
    fn says_why_a_dev_request_failed() {
        let say = |error| dev_failure_message(&error);
        assert_eq!(say(ProxyError::Unreachable("refused".into())), "refused");
        assert_eq!(
            say(ProxyError::Timeout),
            "no response headers after 30000ms"
        );
        assert_eq!(say(ProxyError::Stalled), "no data for 30000ms");
        assert_eq!(say(ProxyError::TooLarge), "over 134217728 bytes");
    }

    #[test]
    fn bounds_a_forwarded_request_at_thirty_seconds_by_default() {
        assert_eq!(PROXY_TIMEOUT_MS, 30000);
        assert_eq!(proto(SERVER).timeout_ms(false), 30000);
    }

    #[test]
    fn gives_a_request_carrying_a_body_the_longer_upload_budget() {
        // The server cannot send headers until it has read the upload, so the
        // transfer is inside the window.
        let protocol = proto(SERVER).with_timeouts(Timeouts {
            proxy_ms: 5,
            upload_ms: 20000,
        });
        assert_eq!(protocol.timeout_ms(true), 20000);
        assert_eq!(protocol.timeout_ms(false), 5);
    }

    #[test]
    fn still_bounds_an_upload_rather_than_waiting_on_it_forever() {
        let protocol = proto(SERVER).with_timeouts(Timeouts {
            proxy_ms: 60000,
            upload_ms: 5,
        });
        assert_eq!(protocol.timeout_ms(true), 5);
    }

    #[test]
    fn allows_two_minutes_for_an_upload_by_default() {
        assert_eq!(UPLOAD_TIMEOUT_MS, 120000);
        assert_eq!(proto(SERVER).timeout_ms(true), 120000);
    }

    #[test]
    fn answers_a_server_that_is_not_a_url_instead_of_failing_the_handler() {
        // The JS throws out of the handler here, which is the freeze.
        for base in ["not a url", "mailto:someone@example.com"] {
            let error = proto(base).proxy_target("/api/v1/tasks", "").unwrap_err();
            assert_eq!(
                error,
                ProxyError::Unreachable("invalid server URL".to_string())
            );
            assert_eq!(proxy_error_reply(&error).status, 502);
        }
    }

    // the drawer frame

    #[test]
    fn serves_the_frame_document_with_a_policy_of_its_own() {
        assert_eq!(route(DRAWER_FRAME_PATH, false), Route::DrawerFrame);
        assert_eq!(route(DRAWER_FRAME_PATH, true), Route::DrawerFrame);

        let reply = drawer_frame_reply("<html>frame</html>", "default-src 'none'");
        assert_eq!(reply.status, 200);
        assert!(reply.header("content-type").unwrap().contains("text/html"));
        assert_eq!(
            reply.header("content-security-policy"),
            Some("default-src 'none'")
        );
        assert_eq!(reply.body, "<html>frame</html>");
    }

    #[test]
    fn never_caches_the_frame_because_the_nonce_inside_it_changes() {
        let reply = drawer_frame_reply("", "default-src 'none'");
        assert_eq!(reply.header("cache-control"), Some("no-store"));
    }

    // It is matched exactly, so it cannot be reached by a path that merely
    // starts the same way, and it does not shadow a real asset.
    #[test]
    fn is_one_path_and_not_a_prefix() {
        let near = format!("{DRAWER_FRAME_PATH}/extra");
        assert_eq!(route(&near, false), Route::Static);
        // The drawer branch is asked before the dev branch, so dev changes nothing.
        assert_eq!(route(&near, true), Route::DevServer);
        assert_eq!(route(DRAWER_FRAME_PATH, true), Route::DrawerFrame);

        // It falls through to the app, which means the app's own policy, not the
        // frame's permissive one, which would be the dangerous way to be wrong.
        let serve = plan_static_request(&near, &|_| false)
            .serve()
            .expect("served");
        let csp = get(
            &serve.headers(&|| proto(SERVER).csp()),
            "content-security-policy",
        )
        .unwrap()
        .to_string();
        assert!(csp.contains("default-src 'self'"));
        assert!(!csp.contains("default-src 'none'"));
    }

    // a drawer's code

    #[test]
    fn routes_a_drawer_by_format_with_the_suffix_and_escapes_removed() {
        assert_eq!(
            route(&format!("{DRAWER_CODE_PREFIX}mermaid.js"), false),
            Route::DrawerCode(Some("mermaid".to_string()))
        );
        assert_eq!(
            route(&format!("{DRAWER_CODE_PREFIX}vega%2Dlite.js"), false),
            Route::DrawerCode(Some("vega-lite".to_string()))
        );
        // Only one trailing `.js` is a suffix.
        assert_eq!(
            route(&format!("{DRAWER_CODE_PREFIX}a.js.js"), false),
            Route::DrawerCode(Some("a.js".to_string()))
        );
        assert_eq!(
            route(&format!("{DRAWER_CODE_PREFIX}plain"), false),
            Route::DrawerCode(Some("plain".to_string()))
        );
    }

    #[test]
    fn a_drawer_name_with_a_malformed_escape_routes_without_a_format() {
        // The JS throws from `decodeURIComponent` and the handler rejects; here
        // `traverses` refuses the escape first, so the name never gets that far.
        assert_eq!(
            route(&format!("{DRAWER_CODE_PREFIX}%zz.js"), false),
            Route::Forbidden
        );
        assert_eq!(decode_uri_component("%zz"), None);
    }

    #[test]
    fn serves_the_code_readable_from_an_opaque_origin() {
        let reply = drawer_code_reply(&DrawerLookup::Found {
            code: "export default () => {}".to_string(),
        });

        assert_eq!(reply.status, 200);
        assert!(reply.header("content-type").unwrap().contains("javascript"));
        // The frame's origin is opaque, so importing a module from it is a
        // cross-origin request that needs CORS to be readable at all.
        assert_eq!(reply.header("access-control-allow-origin"), Some("*"));
        assert!(reply.body.contains("export default"));
    }

    #[test]
    fn does_not_cache_a_drawer_because_what_is_installed_changes_under_the_app() {
        let reply = drawer_code_reply(&DrawerLookup::Found {
            code: String::new(),
        });
        assert_eq!(reply.header("cache-control"), Some("no-store"));
    }

    #[test]
    fn answers_404_for_a_format_nothing_draws_saying_why_when_there_is_a_why() {
        let reply = drawer_code_reply(&DrawerLookup::Refused {
            reason: Some("Nothing draws \"vega-lite\".".to_string()),
        });
        assert_eq!(reply.status, 404);
        assert!(reply.body.contains("Nothing draws"));
    }

    #[test]
    fn still_answers_404_when_the_refusal_carries_no_reason() {
        let reply = drawer_code_reply(&DrawerLookup::Refused { reason: None });
        assert_eq!(reply.status, 404);
        assert_eq!(reply.body, "No such drawer");
    }

    // bundled assets

    fn serve(raw: &str, exists: bool) -> StaticOutcome {
        plan_static_request(raw, &|_| exists)
    }

    #[test]
    fn serves_index_html_at_the_root_with_a_csp_and_no_caching() {
        let serve = serve("/", false).serve().expect("served");
        assert_eq!(serve.file_path, "/index.html");
        let headers = serve.headers(&|| proto(SERVER).csp());
        let calls = Cell::new(0);
        headers_counted(&serve, &calls);
        assert_eq!(calls.get(), 1);
        assert_eq!(
            get(&headers, "content-type"),
            Some("text/html; charset=utf-8")
        );
        assert!(get(&headers, "content-security-policy")
            .unwrap()
            .contains("default-src 'self'"));
        assert_eq!(get(&headers, "cache-control"), Some("no-store"));
    }

    #[test]
    fn serves_an_existing_asset_with_its_own_type_and_no_csp_header() {
        let serve = serve("/assets/app.css", true).serve().expect("served");
        assert_eq!(serve.file_path, "/assets/app.css");
        // The policy is built for the entry only, never for an asset.
        let calls = Cell::new(0);
        let headers = headers_counted(&serve, &calls);
        assert_eq!(calls.get(), 0);
        assert_eq!(
            get(&headers, "content-type"),
            Some("text/css; charset=utf-8")
        );
        assert_eq!(get(&headers, "content-security-policy"), None);
        assert_eq!(get(&headers, "cache-control"), None);
    }

    #[test]
    fn falls_back_to_index_html_for_a_deep_spa_route() {
        let serve = serve("/workspaces/0Zzh/tasks/0hua", false)
            .serve()
            .expect("served");
        assert_eq!(serve.file_path, "/index.html");
        assert!(serve.index);
    }

    #[test]
    fn four_oh_fours_a_missing_asset() {
        assert_eq!(StaticOutcome::NotFound.serve(), None);
        assert_eq!(StaticOutcome::Forbidden.serve(), None);
        assert_eq!(serve("/assets/gone.js", false), StaticOutcome::NotFound);
        assert_eq!(not_found_reply().status, 404);
        assert_eq!(not_found_reply().body, "Not Found");
    }

    #[test]
    fn refuses_a_path_that_decodes_to_a_null_byte() {
        // %00 is one of the few sequences that survives URL parsing intact, so
        // it only becomes dangerous at the point the path is decoded.
        let probe = Probe::default();
        let outcome = plan_with(&probe, "/assets/%00.js");
        assert_eq!(outcome, StaticOutcome::Forbidden);
        assert_eq!(
            probe.asked.get(),
            0,
            "nothing is looked up for a refused path"
        );
        assert_eq!(forbidden_reply().status, 403);
        assert_eq!(forbidden_reply().body, "Forbidden");
    }

    #[test]
    fn refuses_encoded_traversal_which_a_tauri_request_does_not_collapse() {
        // A WHATWG `URL` collapses `%2e%2e` before the handler sees it (the JS
        // case is "never sees encoded traversal"). A Tauri request path is not
        // normalised, so here the guard is the primary control.
        assert_eq!(
            serve("/%2e%2e/%2e%2e/etc/passwd", true),
            StaticOutcome::Forbidden
        );
        assert_eq!(
            serve("/assets/..%2f..%2fsecret", true),
            StaticOutcome::Forbidden
        );
        assert_eq!(serve("/../etc/passwd", true), StaticOutcome::Forbidden);
    }

    #[test]
    fn refuses_backslash_double_slash_drive_letter_and_unc_paths() {
        for raw in [
            "/..%5c..%5cboot",
            "/..%5C..%5Cboot",
            "/..\\..\\boot",
            "/assets/a%5cb.js",
            "/%2f%2fetc%2fpasswd",
            "//etc/passwd",
            "/assets//x.js",
            "/C:/Windows/win",
            "/C%3a/Windows/win",
            "/%5c%5c%3f%5cC%3a%5cx",
            "/%5c%5cserver%5cshare",
            "/assets/a%00b.js",
            "/%252e%252e/%252e%252e/etc/passwd",
            "/assets/%252e%252e%255csecret",
            "/assets/%25252e%25252e/x",
            "/assets/./x.js",
            "/assets/.%2e/x.js",
            "/assets/.\tx",
        ] {
            assert_eq!(serve(raw, true), StaticOutcome::Forbidden, "{raw}");
        }
    }

    /// The review's table: one malformed escape after a `%2e%2e` must not hide it
    /// from `traverses`, because the URL parser collapses the dots anyway.
    const MALFORMED_WITH_DOTS: [&str; 7] = [
        "/api/%2e%2e/healthz%zz",
        "/api/%2e%2e/storage/x%",
        "/api/%2E%2e/storage/x%4",
        "/api/.%2e/healthz%zz",
        "/api/%2e%2e/%2e%2e/etc/passwd%c0",
        "/mcp/%2e%2e/oauth/authorize%ff",
        "/.well-known/%2e%2e/healthz%zz",
    ];

    #[test]
    fn a_malformed_escape_cannot_hide_a_dot_segment() {
        for path in MALFORMED_WITH_DOTS {
            assert!(traverses(path), "{path}");
            for dev in [false, true] {
                assert_eq!(route(path, dev), Route::Forbidden, "{path}");
            }
            assert_eq!(
                proto("https://srv.example:8443/base").proxy_target(path, "?q=1"),
                Err(ProxyError::Forbidden),
                "{path}"
            );
            assert_eq!(
                dev_proto().dev_target(path, "?q=1"),
                Err("Invalid URL".to_string()),
                "{path}"
            );
        }
    }

    #[test]
    fn the_parsed_path_must_keep_the_shape_it_was_given() {
        let base = "https://srv.example:8443/base";
        // The allow-list is asked about the parsed path.
        assert_eq!(
            join_target(base, "/api/x", "", &|path| path == "/api/x"),
            Ok("https://srv.example:8443/api/x".to_string())
        );
        assert_eq!(
            join_target(base, "/api/x", "", &|_| false),
            Err(ProxyError::Forbidden)
        );
        // A path with no leading slash gains one, which is not a changed shape.
        assert_eq!(
            join_target(base, "api/x", "", &|_| true),
            Ok("https://srv.example:8443/api/x".to_string())
        );
    }

    #[test]
    fn refuses_windows_device_names_and_trailing_dots_and_spaces() {
        for raw in [
            "/con",
            "/CON",
            "/assets/aux.js",
            "/COM1.txt",
            "/x/lpt9",
            "/x/nul.tar.gz",
            "/x/..%20",
            "/x/a%20",
            "/x/a.",
            "/x/con%20.js",
        ] {
            assert_eq!(serve(raw, true), StaticOutcome::Forbidden, "{raw}");
        }
        for raw in [
            "/console.js",
            "/assets/com.js",
            "/assets/com10.js",
            "/assets/lpt0",
            "/assets/auxiliary",
            "/assets/a b.js",
        ] {
            assert!(serve(raw, true).serve().is_some(), "{raw}");
        }
    }

    #[test]
    fn serves_names_that_merely_look_unusual() {
        // A trailing slash, a dotted name, and a literal percent are fine.
        for raw in ["/assets/", "/assets/a.b.js", "/assets/100%25.js", "/"] {
            assert!(serve(raw, true).serve().is_some(), "{raw}");
        }
    }

    #[test]
    fn accepts_the_empty_path_which_is_the_index() {
        assert!(is_safe_asset_path(""));
        assert!(serve("", true).serve().unwrap().index);
    }

    #[test]
    fn hands_the_shell_a_root_relative_path() {
        let serve = serve("/assets/app.js", true).serve().unwrap();
        assert_eq!(serve.file_path, "/assets/app.js");
        assert_eq!(serve.relative_path(), "assets/app.js");
    }

    #[test]
    fn normaliser_finds_traversal_at_every_decoding_level() {
        for path in [
            "/a/../b",
            "/a/./b",
            "/a/%2e%2e/b",
            "/a/%2E/b",
            "/a/..%5cb",
            "/a/..\\b",
            "/a/%252e%252e/b",
            "/a/%2525252e%2525252e/b",
            "/a/%00",
            "/a/.\tb",
            "/a/%0a",
        ] {
            assert!(traverses(path), "{path}");
        }
        for path in ["/a/b", "/a/.b/c.", "/a/...", "/a/%2561", "/a/100%25"] {
            assert!(!traverses(path), "{path}");
        }
        // A malformed escape is refused, deliberately: see `traverses`.
        for path in ["/a/%zz", "/a/100%", "/a/%c0", "/a/%4"] {
            assert!(traverses(path), "{path}");
        }
        for segment in [".", "%2e", "%2E", "..", ".%2e", "%2e.", "%2E%2e", ".%2E"] {
            assert!(is_dot_segment(segment), "{segment}");
            assert!(traverses(&format!("/a/{segment}/b")), "{segment}");
        }
        for segment in ["...", "%2e%2e%2e", "a.", ".a", "%2f", ""] {
            assert!(!is_dot_segment(segment), "{segment}");
        }
        assert!(traverses("/a/%252525252525252e%252525252525252e/b"));
    }

    #[test]
    fn refuses_a_path_with_a_malformed_escape_sequence() {
        let probe = Probe::default();
        let outcome = plan_with(&probe, "/assets/%zz.js");
        assert_eq!(outcome, StaticOutcome::Forbidden);
        assert_eq!(probe.asked.get(), 0);
    }

    #[test]
    fn decodes_the_path_before_looking_it_up_on_disk() {
        // Without decoding, this asset would be searched for under a literal '%20'.
        let probe = Probe::default();
        let outcome = plan_with(&probe, "/assets/my%20file.js");
        assert_eq!(*probe.paths.borrow(), ["/assets/my file.js"]);
        assert_eq!(
            outcome,
            StaticOutcome::Serve(StaticServe {
                file_path: "/assets/my file.js".to_string(),
                index: false
            })
        );
    }

    // The allow-list is checked on what the path means, not on its spelling.

    const ESCAPES: [(&str, &str); 9] = [
        ("/api/../healthz", ""),
        ("/api/%2e%2e/storage/x", ""),
        ("/mcp/%2e%2e/%2e%2e/etc", ""),
        ("/mcp/../oauth/authorize", "?a=1"),
        ("/.well-known/../x", ""),
        ("/api/..%5chealthz", ""),
        ("/api/%252e%252e/x", ""),
        ("/api/./v1", ""),
        ("/api/v1/%2E%2E/%2E%2E/x", ""),
    ];

    #[test]
    fn refuses_every_dot_segment_before_routing_and_builds_no_url() {
        for dev in [false, true] {
            for (path, _) in ESCAPES {
                assert_eq!(route(path, dev), Route::Forbidden, "{path}");
            }
        }
        for (path, search) in ESCAPES {
            assert_eq!(
                proto(SERVER).proxy_target(path, search),
                Err(ProxyError::Forbidden),
                "{path}"
            );
            assert_eq!(
                dev_proto().dev_target(path, search),
                Err("Invalid URL".to_string()),
                "{path}"
            );
        }
        let reply = proxy_error_reply(&ProxyError::Forbidden);
        assert_eq!((reply.status, reply.body.as_str()), (403, "Forbidden"));
    }

    #[test]
    fn a_forwarded_path_is_in_the_allow_list_and_keeps_the_base_origin() {
        let base = "https://srv.example:8443/base";
        let protocol = proto(base);
        assert_eq!(
            protocol.proxy_target("/api/v1/tasks", "?a=1&b=%2e"),
            Ok("https://srv.example:8443/api/v1/tasks?a=1&b=%2e".to_string())
        );
        // A name that merely contains dots, and an escaped slash, stay as sent.
        assert_eq!(
            protocol.proxy_target("/api/v1/a..b/%2Fc", ""),
            Ok("https://srv.example:8443/api/v1/a..b/%2Fc".to_string())
        );
        // Outside the allow-list there is no target, whichever way it is asked.
        for path in [
            "/healthz",
            "/storage/x",
            "/etc",
            "/apix",
            "//evil.example/api/",
        ] {
            assert_eq!(
                protocol.proxy_target(path, ""),
                Err(ProxyError::Forbidden),
                "{path}"
            );
        }
    }

    #[test]
    fn a_double_slash_cannot_change_the_dev_target_host() {
        assert_eq!(
            dev_proto().dev_target("//evil.example/x", ""),
            Ok("http://localhost:5174//evil.example/x".to_string())
        );
    }

    #[test]
    fn a_base_that_cannot_hold_a_path_is_unreachable() {
        assert_eq!(
            proto("mailto:a@b.example").proxy_target("/api/v1/x", ""),
            Err(ProxyError::Unreachable("invalid server URL".to_string()))
        );
    }

    #[test]
    fn a_fragment_in_the_search_is_never_sent() {
        assert_eq!(
            proto(SERVER).proxy_target("/api/v1/x", "?a=1#frag"),
            Ok("http://localhost:3000/api/v1/x?a=1%23frag".to_string())
        );
    }

    #[test]
    fn mcp_is_a_bare_prefix_so_a_longer_name_is_proxied_as_the_js_does() {
        assert!(is_proxy_path("/mcpx"));
        assert_eq!(route("/mcpx", false), Route::Proxy);
    }

    // attachments

    #[test]
    fn serves_from_the_store_only_a_proxied_get_of_an_attachment() {
        let path = "/api/v1/workspaces/w/tasks/t/attachments/a";
        assert!(serve_from_store("GET", path));
        assert!(!serve_from_store("POST", path));
        assert!(!serve_from_store("get", path));
        // The events stream and the task list are forwarded untouched.
        assert!(!serve_from_store(
            "GET",
            "/api/v1/workspaces/w/tasks/t/events"
        ));
        assert!(!serve_from_store("GET", "/api/v1/workspaces/w/tasks"));
        assert!(!serve_from_store(
            "GET",
            "/api/v1/workspaces/w/tasks//attachments/a"
        ));
        assert!(!serve_from_store(
            "GET",
            "/api/v1/workspaces/w/tasks/t/attachments/"
        ));
        // Not under a proxied prefix, so never the store's business.
        assert!(!serve_from_store("GET", "/x/tasks/a/attachments/b"));
        assert!(!serve_from_store("GET", "/api/../x/tasks/a/attachments/b"));
    }

    #[test]
    fn replies_to_a_stored_attachment_with_its_type_and_disposition() {
        let reply =
            attachment_hit_reply(b"png".to_vec(), "image/png", "inline; filename=\"a.png\"");
        assert_eq!(reply.status, 200);
        assert_eq!(reply.body, b"png");
        assert_eq!(
            names(&reply.headers),
            [
                "content-type",
                "content-disposition",
                "x-content-type-options",
                "content-security-policy"
            ]
        );
        assert_eq!(get(&reply.headers, "content-type"), Some("image/png"));
        assert_eq!(
            get(&reply.headers, "content-disposition"),
            Some("inline; filename=\"a.png\"")
        );
    }

    #[test]
    fn leaves_out_an_empty_disposition() {
        let reply = attachment_hit_reply(Vec::new(), "text/plain", "");
        assert_eq!(get(&reply.headers, "content-disposition"), None);
    }

    #[test]
    fn sandboxes_a_stored_attachment_so_framed_html_cannot_run_as_the_app() {
        let reply = attachment_hit_reply(b"<script>".to_vec(), "text/html;pdf", "inline");
        assert_eq!(
            get(&reply.headers, "content-security-policy"),
            Some("sandbox; default-src 'none'")
        );
        assert_eq!(
            get(&reply.headers, "x-content-type-options"),
            Some("nosniff")
        );
    }

    #[test]
    fn answers_502_when_the_body_cannot_be_read_to_the_end() {
        let reply = bad_gateway_reply();
        assert_eq!((reply.status, reply.body.as_str()), (502, "Bad Gateway"));
    }

    #[test]
    fn stores_an_absent_disposition_as_empty() {
        assert_eq!(stored_content_disposition(None), "");
        assert_eq!(stored_content_disposition(Some("inline")), "inline");
    }

    // drawers

    #[test]
    fn answers_a_missing_drawer_source_with_the_extensions_message() {
        assert_eq!(NO_DRAWER_SOURCE, "Extensions are unavailable.");
        let reply = drawer_code_reply(&DrawerLookup::default());
        assert_eq!(reply.status, 404);
        assert_eq!(reply.body, NO_DRAWER_SOURCE);
    }

    // a host cannot inject into the policy

    #[test]
    fn gives_a_host_that_could_inject_a_directive_no_entry() {
        for url in [
            "http://a;script-src.evil/",
            "https://a,b.example/",
            "http://a'b.example:80/",
            "http://under_score.example/",
            "http://[::1;x]/",
        ] {
            assert_eq!(web_socket_origin(url), "", "{url}");
        }
        let csp = build_csp(&opts(false, "", "http://a;script-src.evil/"));
        assert!(!directive(&csp, "connect-src").contains(';'));
        assert!(!csp.contains("evil"));
    }

    #[test]
    fn a_host_with_an_underscore_gets_no_entry() {
        // The CSP host grammar is alphanumerics and `-`, so a browser ignores a
        // source with `_`. The JS lists it all the same; a deliberate divergence.
        for url in ["http://my_host:3000", "http://agentrq_backend:3000"] {
            assert_eq!(web_socket_origin(url), "", "{url}");
        }
    }

    #[test]
    fn still_lists_names_and_ipv6_literals() {
        assert_eq!(
            web_socket_origin("https://Srv-1.example:8443/x"),
            "wss://srv-1.example:8443"
        );
        assert_eq!(web_socket_origin("http://[::1]:8443"), "ws://[::1]:8443");
        assert_eq!(
            web_socket_origin("http://[::ffff:127.0.0.1]/"),
            "ws://[::ffff:7f00:1]"
        );
        assert_eq!(
            web_socket_origin("http://127.0.0.1:3000"),
            "ws://127.0.0.1:3000"
        );
    }

    // dev server mode

    #[test]
    fn sources_assets_from_vite_while_still_proxying_the_api() {
        let protocol = dev_proto();
        assert!(protocol.dev());
        assert_eq!(protocol.route("/src/main.js"), Route::DevServer);
        assert_eq!(
            protocol.dev_target("/src/main.js", ""),
            Ok("http://localhost:5174/src/main.js".to_string())
        );

        assert_eq!(protocol.route("/api/v1/auth/user"), Route::Proxy);
        assert_eq!(
            protocol.proxy_target("/api/v1/auth/user", ""),
            Ok("http://localhost:3000/api/v1/auth/user".to_string())
        );
    }

    #[test]
    fn routes_to_disk_when_there_is_no_dev_server() {
        let protocol = proto(SERVER);
        assert!(!protocol.dev());
        assert_eq!(protocol.route("/src/main.js"), Route::Static);
        assert_eq!(protocol.route("/mcp/ws1"), Route::Proxy);
        assert_eq!(protocol.route("/.well-known/x"), Route::Proxy);
    }

    #[test]
    fn applies_the_dev_csp_to_html_from_the_dev_server() {
        let headers = dev_response_headers(pairs(&[("content-type", "text/html")]), &|| {
            dev_proto().csp()
        });
        assert!(get(&headers, "content-security-policy")
            .unwrap()
            .contains("'unsafe-eval'"));

        let calls = Cell::new(0);
        let upstream = pairs(&[("content-type", "text/html")]);
        dev_headers_counted(upstream, &calls);
        assert_eq!(calls.get(), 1);
    }

    #[test]
    fn leaves_non_html_dev_responses_without_a_csp_header() {
        let calls = Cell::new(0);
        let upstream = pairs(&[("content-type", "text/javascript")]);
        let headers = dev_headers_counted(upstream, &calls);
        assert_eq!(calls.get(), 0);
        assert_eq!(get(&headers, "content-security-policy"), None);
    }

    #[test]
    fn tolerates_a_dev_response_with_no_content_type() {
        let calls = Cell::new(0);
        let headers = dev_headers_counted(pairs(&[("x-a", "b")]), &calls);
        assert_eq!(calls.get(), 0);
        assert_eq!(names(&headers), ["x-a"]);
    }

    #[test]
    fn replaces_a_policy_the_dev_server_sent_and_filters_like_the_proxy() {
        let headers = dev_response_headers(
            pairs(&[
                ("content-type", "text/html; charset=utf-8"),
                ("content-security-policy", "default-src *"),
                ("content-encoding", "gzip"),
                ("set-cookie", "a=b"),
            ]),
            &|| "mine".to_string(),
        );
        assert_eq!(
            headers,
            [
                (
                    "content-type".to_string(),
                    "text/html; charset=utf-8".to_string()
                ),
                ("content-security-policy".to_string(), "mine".to_string())
            ]
        );
    }

    #[test]
    fn forwards_the_query_string_to_the_dev_server() {
        assert_eq!(
            dev_proto().dev_target("/src/App.vue", "?vue&type=style"),
            Ok("http://localhost:5174/src/App.vue?vue&type=style".to_string())
        );
    }

    #[test]
    fn answers_502_rather_than_failing_when_the_dev_server_is_down() {
        // Unhandled this rejects the handler, and a rejected handler leaves the
        // renderer with a request that never completes.
        let reply = dev_unreachable_reply("ECONNREFUSED");
        assert_eq!(reply.status, 502);
        assert_eq!(reply.body, "Cannot reach the dev server: ECONNREFUSED");
    }

    #[test]
    fn answers_502_rather_than_hanging_when_the_dev_server_never_replies() {
        let ms = dev_proto().timeout_ms(false);
        let reply = dev_unreachable_reply(&timeout_message(ms));
        assert_eq!(reply.status, 502);
        assert_eq!(
            reply.body,
            "Cannot reach the dev server: no response headers after 30000ms"
        );
    }

    #[test]
    fn answers_502_for_a_dev_server_url_that_is_not_a_url() {
        let protocol = dev_proto_at("not a url");
        assert_eq!(
            protocol.dev_target("/x", ""),
            Err("Invalid URL".to_string())
        );
    }
}
