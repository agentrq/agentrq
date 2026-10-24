// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The `app` scheme the renderer is served from, as far as WP1 takes it.
//!
//! It serves the built renderer and nothing else. WP2 replaces this module with
//! the real handler (path checks, SPA rules, CSP, the API proxy and its
//! per-profile cookie jar); until then the proxied paths answer 503, so the
//! renderer sees the same failure as an Electron app with no server.

use http::Response;

/// Where a request on the scheme goes.
#[derive(Debug, PartialEq, Eq)]
pub enum Route {
    /// To the configured server, through the proxy (WP2).
    Proxy,
    /// To a file of the built renderer, by its path inside `dist/renderer`.
    Asset(String),
}

/// The paths Electron's protocol handler forwards to the server.
pub fn route(path: &str) -> Route {
    if path.starts_with("/api/")
        || path == "/mcp"
        || path.starts_with("/mcp/")
        || path.starts_with("/.well-known/")
    {
        return Route::Proxy;
    }
    match path.trim_start_matches('/') {
        "" => Route::Asset("index.html".into()),
        rest => Route::Asset(rest.into()),
    }
}

/// The answer to a proxied path until WP2 lands.
pub fn proxy_pending() -> Response<Vec<u8>> {
    Response::builder()
        .status(503)
        .header("content-type", "text/plain; charset=utf-8")
        .body(b"no server: the proxy is not implemented yet (WP2)".to_vec())
        .expect("static response")
}

/// A renderer file, or 404. HTML is never cached, so a new build shows at once.
pub fn asset(found: Option<(Vec<u8>, String)>) -> Response<Vec<u8>> {
    match found {
        Some((bytes, mime)) => {
            let builder = Response::builder().status(200);
            let builder = if mime.starts_with("text/html") {
                builder.header("cache-control", "no-store")
            } else {
                builder
            };
            builder
                .header("content-type", mime)
                .body(bytes)
                .expect("valid content type")
        }
        None => Response::builder()
            .status(404)
            .body(b"not found".to_vec())
            .expect("static response"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sends_the_server_paths_to_the_proxy() {
        for path in [
            "/api/v1/auth/config",
            "/mcp",
            "/mcp/ws1",
            "/.well-known/oauth",
        ] {
            assert_eq!(route(path), Route::Proxy, "{path}");
        }
    }

    #[test]
    fn serves_everything_else_from_the_renderer() {
        assert_eq!(route("/"), Route::Asset("index.html".into()));
        assert_eq!(route(""), Route::Asset("index.html".into()));
        assert_eq!(
            route("/assets/main.js"),
            Route::Asset("assets/main.js".into())
        );
        assert_eq!(route("/mcpx"), Route::Asset("mcpx".into()));
        assert_eq!(route("/apix/y"), Route::Asset("apix/y".into()));
    }

    #[test]
    fn answers_503_for_the_proxy_until_wp2() {
        let res = proxy_pending();
        assert_eq!(res.status(), 503);
        assert!(String::from_utf8_lossy(res.body()).contains("WP2"));
    }

    #[test]
    fn serves_a_file_with_its_type_and_never_caches_html() {
        let html = asset(Some((b"<html>".to_vec(), "text/html".into())));
        assert_eq!(html.status(), 200);
        assert_eq!(html.headers()["content-type"], "text/html");
        assert_eq!(html.headers()["cache-control"], "no-store");
        assert_eq!(html.body(), b"<html>");

        let js = asset(Some((b"x".to_vec(), "text/javascript".into())));
        assert_eq!(js.headers()["content-type"], "text/javascript");
        assert!(js.headers().get("cache-control").is_none());
    }

    #[test]
    fn answers_404_for_a_missing_file() {
        assert_eq!(asset(None).status(), 404);
    }
}
