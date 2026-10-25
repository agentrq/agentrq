// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! One request to the server (or to Vite), bounded by a deadline.
//!
//! Every wait is a race, never only an abort (docs/agents/desktop.md): the
//! scheme's responder is what the renderer's `fetch` waits on, so a future
//! that never settles is a frozen app. Losing a race drops the request. The
//! headers get one budget (`core::protocol::PROXY_TIMEOUT_MS`); the body then
//! gets an idle deadline per read rather than a total, so a long download is
//! not cut off but one that stops is, and a cap on its size, because it is
//! buffered whole.
//!
//! The scheme buffers every response (WP0 S1), so nothing streamed goes this
//! way: the event stream has its own path, `event_source`.

use std::time::Duration;

use agentrq_desktop_core::protocol::{
    HeaderList, ProxyError, MAX_PROXIED_BODY_BYTES, PROXY_IDLE_TIMEOUT_MS,
};
use futures_util::StreamExt;
use reqwest::header::{HeaderMap, HeaderName, HeaderValue};

/// Request headers that never go upstream, beyond the ones core strips. The
/// client sends its own cookies from the profile's jar, and negotiates and
/// decodes its own compression, so the body handed back is the plain one core
/// expects when it drops `content-encoding`.
const CLIENT_OWNED: [&str; 2] = ["cookie", "accept-encoding"];

/// A request to send.
pub struct Outgoing {
    pub method: http::Method,
    pub url: String,
    /// Already filtered with `core::protocol::filter_request_headers`.
    pub headers: HeaderList,
    pub body: Option<Vec<u8>>,
    pub timeout_ms: u64,
    pub limits: BodyLimits,
}

/// What the answer's body may do once its headers are in.
#[derive(Debug, Clone, Copy)]
pub struct BodyLimits {
    /// The longest silence between two reads.
    pub idle_ms: u64,
    pub max_bytes: u64,
}

impl Default for BodyLimits {
    fn default() -> Self {
        Self {
            idle_ms: PROXY_IDLE_TIMEOUT_MS,
            max_bytes: MAX_PROXIED_BODY_BYTES,
        }
    }
}

/// What came back, headers unfiltered (names lowercase). The body is an error
/// when it broke off, stalled or grew too large after the headers had arrived.
#[derive(Debug)]
pub struct Incoming {
    pub status: u16,
    pub headers: HeaderList,
    pub body: Result<Vec<u8>, ProxyError>,
}

impl Incoming {
    pub fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(key, _)| key == name)
            .map(|(_, value)| value.as_str())
    }
}

/// The header list as pairs of string slices, for core's filters.
pub fn pairs(headers: &HeaderList) -> impl Iterator<Item = (&str, &str)> {
    headers.iter().map(|(n, v)| (n.as_str(), v.as_str()))
}

fn header_map(headers: &HeaderList) -> HeaderMap {
    headers
        .iter()
        .filter(|(name, _)| !CLIENT_OWNED.contains(&name.as_str()))
        .filter_map(|(name, value)| {
            Some((
                HeaderName::from_bytes(name.as_bytes()).ok()?,
                HeaderValue::from_str(value).ok()?,
            ))
        })
        .collect()
}

/// Send `out` and read the whole answer. `Err` when no headers came back.
pub async fn send(client: &reqwest::Client, out: Outgoing) -> Result<Incoming, ProxyError> {
    let mut request = client
        .request(out.method, &out.url)
        .headers(header_map(&out.headers));
    if let Some(body) = out.body {
        request = request.body(body);
    }
    let headers = tokio::time::timeout(Duration::from_millis(out.timeout_ms), request.send());
    let response = match headers.await {
        Err(_) => return Err(ProxyError::Timeout),
        Ok(Err(error)) => return Err(ProxyError::Unreachable(error.to_string())),
        Ok(Ok(response)) => response,
    };
    let status = response.status().as_u16();
    let headers = response
        .headers()
        .iter()
        .filter_map(|(name, value)| Some((name.to_string(), value.to_str().ok()?.to_string())))
        .collect();
    let body = read_body(response, out.limits).await;
    Ok(Incoming {
        status,
        headers,
        body,
    })
}

async fn read_body(response: reqwest::Response, limits: BodyLimits) -> Result<Vec<u8>, ProxyError> {
    if response.content_length().unwrap_or(0) > limits.max_bytes {
        return Err(ProxyError::TooLarge);
    }
    let idle = Duration::from_millis(limits.idle_ms);
    let mut stream = response.bytes_stream();
    let mut body = Vec::new();
    loop {
        match tokio::time::timeout(idle, stream.next()).await {
            Err(_) => return Err(ProxyError::Stalled),
            Ok(None) => return Ok(body),
            Ok(Some(Err(error))) => return Err(ProxyError::Unreachable(error.to_string())),
            Ok(Some(Ok(chunk))) => {
                if (body.len() + chunk.len()) as u64 > limits.max_bytes {
                    return Err(ProxyError::TooLarge);
                }
                body.extend_from_slice(&chunk);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::test_server::{closed_port, reply, send as step, Step, TestServer};

    fn get(url: String, timeout_ms: u64) -> Outgoing {
        Outgoing {
            method: http::Method::GET,
            url,
            headers: Vec::new(),
            body: None,
            timeout_ms,
            limits: BodyLimits::default(),
        }
    }

    #[tokio::test]
    async fn passes_the_upstream_status_and_body_straight_through() {
        let server = TestServer::start(|_| {
            reply("418 I'm a teapot", &["x-a: 1", "x-bad: caf\u{e9}"], "short")
        })
        .await;
        let answer = send(
            &reqwest::Client::new(),
            get(format!("{}/api/v1/x", server.url), 1000),
        )
        .await
        .unwrap();
        assert_eq!(answer.status, 418);
        assert_eq!(answer.header("x-a"), Some("1"));
        assert_eq!(answer.header("nope"), None);
        assert_eq!(
            answer.header("x-bad"),
            None,
            "not a string, so not passed on"
        );
        assert_eq!(answer.body.unwrap(), b"short");
    }

    #[tokio::test]
    async fn sends_a_request_body_and_the_headers_it_was_given() {
        let server = TestServer::start(|_| reply("201 Created", &[], "")).await;
        let out = Outgoing {
            method: http::Method::POST,
            url: format!("{}/api/v1/tasks", server.url),
            headers: vec![
                ("content-type".into(), "application/json".into()),
                ("cookie".into(), "at=from-the-page".into()),
                ("accept-encoding".into(), "identity".into()),
                ("bad name".into(), "x".into()),
                ("x-bad-value".into(), "a\nb".into()),
            ],
            body: Some(b"{\"a\":1}".to_vec()),
            timeout_ms: 1000,
            limits: BodyLimits::default(),
        };
        assert_eq!(
            send(&reqwest::Client::new(), out).await.unwrap().status,
            201
        );
        let seen = &server.requests()[0];
        assert_eq!(seen.method, "POST");
        assert_eq!(seen.body, b"{\"a\":1}");
        assert_eq!(seen.header("content-type"), Some("application/json"));
        assert_eq!(seen.header("cookie"), None, "the jar's, never the page's");
        assert_ne!(seen.header("accept-encoding"), Some("identity"));
        assert_eq!(seen.header("x-bad-value"), None);
    }

    #[tokio::test]
    async fn handles_redirects_itself() {
        let server = TestServer::start(|_| reply("302 Found", &["location: /login"], "")).await;
        let answer = send(
            &reqwest::Client::builder()
                .redirect(reqwest::redirect::Policy::none())
                .build()
                .unwrap(),
            get(format!("{}/api/v1/auth/x", server.url), 1000),
        )
        .await
        .unwrap();
        assert_eq!(answer.status, 302);
        assert_eq!(answer.header("location"), Some("/login"));
        assert_eq!(server.requests().len(), 1);
    }

    #[tokio::test]
    async fn aborts_the_request_it_gave_up_on() {
        let server = TestServer::start(|_| vec![Step::Wait(5_000)]).await;
        let started = std::time::Instant::now();
        let failed = send(
            &reqwest::Client::new(),
            get(format!("{}/api/v1/slow", server.url), 100),
        )
        .await;
        assert_eq!(failed.unwrap_err(), ProxyError::Timeout);
        assert!(started.elapsed() < Duration::from_secs(2));
    }

    #[tokio::test]
    async fn does_not_cut_off_a_body_that_arrives_over_longer_than_the_headers_budget() {
        let server = TestServer::start(|_| {
            vec![
                step("HTTP/1.1 200 OK\r\ncontent-length: 4\r\nconnection: close\r\n\r\nab"),
                Step::Wait(400),
                step("cd"),
            ]
        })
        .await;
        let answer = send(
            &reqwest::Client::new(),
            get(format!("{}/api/v1/big", server.url), 150),
        )
        .await
        .unwrap();
        assert_eq!(answer.body.unwrap(), b"abcd");
    }

    #[tokio::test]
    async fn reports_an_unreachable_server_with_what_went_wrong() {
        let failed = send(
            &reqwest::Client::new(),
            // Windows takes ~2 s to give up on a refused connection.
            get(format!("{}/api/v1/x", closed_port().await), 30_000),
        )
        .await;
        assert!(matches!(failed, Err(ProxyError::Unreachable(detail)) if !detail.is_empty()));
    }

    #[tokio::test]
    async fn reports_a_body_cut_short_after_the_headers() {
        let server = TestServer::start(|_| {
            vec![step(
                "HTTP/1.1 200 OK\r\ncontent-length: 10\r\nconnection: close\r\n\r\nab",
            )]
        })
        .await;
        let answer = send(
            &reqwest::Client::new(),
            get(format!("{}/x", server.url), 1000),
        )
        .await
        .unwrap();
        assert_eq!(answer.status, 200);
        assert!(matches!(answer.body, Err(ProxyError::Unreachable(_))));
    }

    #[tokio::test]
    async fn gives_up_on_a_body_that_goes_silent_after_the_headers() {
        // Real time: a paused clock jumps ahead while the socket is still busy.
        let server = TestServer::start(|_| {
            vec![
                step("HTTP/1.1 200 OK\r\ncontent-length: 4\r\nconnection: close\r\n\r\nab"),
                Step::Wait(5_000),
            ]
        })
        .await;
        let mut out = get(format!("{}/api/v1/x", server.url), 1000);
        out.limits.idle_ms = 150;
        let started = std::time::Instant::now();
        let answer = send(&reqwest::Client::new(), out).await.unwrap();
        assert_eq!(answer.body.unwrap_err(), ProxyError::Stalled);
        assert!(started.elapsed() < Duration::from_secs(2));
    }

    #[tokio::test]
    async fn refuses_a_body_over_the_cap_whether_declared_or_not() {
        let declared = TestServer::start(|_| reply("200 OK", &[], "0123456789")).await;
        let chunked = TestServer::start(|_| {
            vec![step(
                "HTTP/1.1 200 OK\r\ntransfer-encoding: chunked\r\nconnection: close\r\n\r\n4\r\n0123\r\n6\r\n456789\r\n0\r\n\r\n",
            )]
        })
        .await;
        for url in [&declared.url, &chunked.url] {
            let mut out = get(format!("{url}/x"), 1000);
            out.limits.max_bytes = 9;
            let answer = send(&reqwest::Client::new(), out).await.unwrap();
            assert_eq!(answer.body.unwrap_err(), ProxyError::TooLarge, "{url}");

            let mut out = get(format!("{url}/x"), 1000);
            out.limits.max_bytes = 10;
            let answer = send(&reqwest::Client::new(), out).await.unwrap();
            assert_eq!(answer.body.unwrap(), b"0123456789", "exactly the cap");
        }
    }

    #[test]
    fn hands_core_the_headers_as_pairs() {
        let list = vec![("a".to_string(), "1".to_string())];
        assert_eq!(pairs(&list).collect::<Vec<_>>(), [("a", "1")]);
    }
}
