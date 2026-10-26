// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! A scripted HTTP/1.1 server on a loopback port, for the tests of everything
//! that talks to the AgentRQ server. Each connection answers one request with
//! the steps the script returns for it, then closes.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

/// A request as the server read it. Header names are lowercase.
#[derive(Debug, Clone)]
pub struct Seen {
    pub method: String,
    pub target: String,
    pub headers: Vec<(String, String)>,
    pub body: Vec<u8>,
}

impl Seen {
    pub fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(n, _)| n == name)
            .map(|(_, v)| v.as_str())
    }
}

/// What the server does, in order.
#[derive(Clone)]
pub enum Step {
    Send(Vec<u8>),
    Wait(u64),
}

pub fn send(text: &str) -> Step {
    Step::Send(text.as_bytes().to_vec())
}

/// A whole response with a body and a length.
pub fn reply(status: &str, headers: &[&str], body: &str) -> Vec<Step> {
    let mut head = format!("HTTP/1.1 {status}\r\ncontent-length: {}\r\n", body.len());
    for header in headers {
        head.push_str(header);
        head.push_str("\r\n");
    }
    vec![send(&format!("{head}connection: close\r\n\r\n{body}"))]
}

type Script = dyn Fn(&Seen) -> Vec<Step> + Send + Sync;

pub struct TestServer {
    pub url: String,
    pub seen: Arc<Mutex<Vec<Seen>>>,
}

impl TestServer {
    pub async fn start(script: impl Fn(&Seen) -> Vec<Step> + Send + Sync + 'static) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let seen = Arc::new(Mutex::new(Vec::new()));
        let script: Arc<Script> = Arc::new(script);
        let log = seen.clone();
        tokio::spawn(async move {
            while let Ok((socket, _)) = listener.accept().await {
                tokio::spawn(serve(socket, script.clone(), log.clone()));
            }
        });
        Self { url, seen }
    }

    pub fn requests(&self) -> Vec<Seen> {
        self.seen.lock().unwrap().clone()
    }
}

async fn serve(mut socket: tokio::net::TcpStream, script: Arc<Script>, log: Arc<Mutex<Vec<Seen>>>) {
    let mut buf = Vec::new();
    let mut chunk = [0u8; 4096];
    let end = loop {
        if let Some(at) = buf.windows(4).position(|w| w == b"\r\n\r\n") {
            break at;
        }
        match socket.read(&mut chunk).await {
            Ok(0) | Err(_) => return,
            Ok(n) => buf.extend_from_slice(&chunk[..n]),
        }
    };
    let head = String::from_utf8_lossy(&buf[..end]).to_string();
    let mut lines = head.split("\r\n");
    let mut first = lines.next().unwrap_or_default().split(' ');
    let method = first.next().unwrap_or_default().to_string();
    let target = first.next().unwrap_or_default().to_string();
    let headers: Vec<(String, String)> = lines
        .filter_map(|line| line.split_once(':'))
        .map(|(n, v)| (n.trim().to_ascii_lowercase(), v.trim().to_string()))
        .collect();
    let length: usize = headers
        .iter()
        .find(|(n, _)| n == "content-length")
        .and_then(|(_, v)| v.parse().ok())
        .unwrap_or(0);
    let mut body = buf[end + 4..].to_vec();
    while body.len() < length {
        match socket.read(&mut chunk).await {
            Ok(0) | Err(_) => break,
            Ok(n) => body.extend_from_slice(&chunk[..n]),
        }
    }
    let seen = Seen {
        method,
        target,
        headers,
        body,
    };
    log.lock().unwrap().push(seen.clone());
    for step in script(&seen) {
        match step {
            Step::Send(bytes) => {
                if socket.write_all(&bytes).await.is_err() {
                    return;
                }
                let _ = socket.flush().await;
            }
            Step::Wait(ms) => tokio::time::sleep(Duration::from_millis(ms)).await,
        }
    }
}

/// A loopback address nothing listens on.
pub async fn closed_port() -> String {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    drop(listener);
    url
}
