// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! On-demand downloading, verification, and installation of Node.js for the extension host.

use std::path::{Path, PathBuf};
use std::sync::Arc;

use agentrq_desktop_core::ext_node::{
    compute_sha256, current_pin, download_url, format_stamp, node_relative_path, parse_stamp,
    validate_stamp, NODE_DISPLAY_SIZE, NODE_VERSION,
};
use futures_util::StreamExt;
use sha2::{Digest, Sha256};
use tokio::io::AsyncWriteExt;
use tokio::sync::Mutex;

static DOWNLOAD_LOCK: Mutex<()> = Mutex::const_new(());

pub(crate) fn err_str<E: std::fmt::Display>(e: E) -> String {
    e.to_string()
}

pub(crate) fn unsupported_platform() -> String {
    "Unsupported platform for Node.js.".to_string()
}

/// The expected path of the installed Node binary under `data_dir``.
pub fn node_binary_path(data_dir: &Path) -> PathBuf {
    data_dir.join(node_relative_path(std::env::consts::OS))
}

/// The verification stamp file recording the binary hash, size, and archive hash.
pub fn stamp_path(data_dir: &Path) -> PathBuf {
    data_dir.join("bin").join("node.sha256")
}

/// Check whether the installed Node binary exists and matches its verification stamp against a specific pin.
pub fn verify_node_with_pin(
    data_dir: &Path,
    pin: &agentrq_desktop_core::ext_node::NodePin,
) -> Result<PathBuf, String> {
    let binary = node_binary_path(data_dir);
    let stamp_file = stamp_path(data_dir);
    if !binary.exists() {
        return Err("Node.js runtime is not installed.".to_string());
    }
    if !stamp_file.exists() {
        return Err("Node.js runtime verification stamp is missing.".to_string());
    }
    let content = std::fs::read_to_string(&stamp_file).map_err(err_str)?;
    let stamp =
        parse_stamp(&content).ok_or_else(|| "Corrupted Node.js verification stamp.".to_string())?;

    let meta = std::fs::metadata(&binary).map_err(err_str)?;
    let actual_size = meta.len();

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = meta.permissions().mode();
        // Check that write permissions are not present (stored read-only)
        if mode & 0o222 != 0 {
            return Err("Node.js binary permissions are not read-only.".to_string());
        }
    }

    let bytes = std::fs::read(&binary).map_err(err_str)?;
    let actual_sha256 = compute_sha256(&bytes);

    validate_stamp(&stamp, pin.sha256, actual_size, &actual_sha256).map_err(err_str)?;

    Ok(binary)
}

/// Check whether the installed Node binary exists and matches its verification stamp.
pub fn verify_node(data_dir: &Path) -> Result<PathBuf, String> {
    let pin = current_pin().ok_or_else(unsupported_platform)?;
    verify_node_with_pin(data_dir, &pin)
}

/// Download, verify, and install Node.js under `data_dir/bin`.
pub async fn download_and_install_node(
    data_dir: &Path,
    client: Option<&reqwest::Client>,
    base_url: Option<&str>,
    on_progress: Option<Arc<dyn Fn(u64, u64) + Send + Sync>>,
    pin_override: Option<&agentrq_desktop_core::ext_node::NodePin>,
) -> Result<PathBuf, String> {
    let _guard = DOWNLOAD_LOCK.lock().await;

    // If already verified, return without downloading again.
    if let Ok(bin) = verify_node(data_dir) {
        return Ok(bin);
    }

    let detected_pin;
    let pin = match pin_override {
        Some(p) => p,
        None => {
            detected_pin = current_pin().ok_or_else(unsupported_platform)?;
            &detected_pin
        }
    };
    let url = match base_url {
        Some(base) => format!("{base}/{}", pin.file),
        None => download_url(pin.file),
    };

    let bin_dir = data_dir.join("bin");
    tokio::fs::create_dir_all(&bin_dir).await.map_err(err_str)?;

    let random_id = unique_temp_id();
    let temp_archive = bin_dir.join(format!(".node-download-{random_id}.tmp"));
    let temp_extract_dir = bin_dir.join(format!(".node-extract-{random_id}.tmp"));
    let temp_binary = bin_dir.join(format!(".node-bin-{random_id}.tmp"));

    let result = download_extract_verify(
        &url,
        pin,
        &temp_archive,
        &temp_extract_dir,
        &temp_binary,
        client,
        on_progress,
    )
    .await;

    // Clean up archive and extraction temp files regardless of outcome.
    let _ = tokio::fs::remove_file(&temp_archive).await;
    let _ = tokio::fs::remove_dir_all(&temp_extract_dir).await;

    match result {
        Ok((bin_size, bin_sha256)) => {
            let final_bin = node_binary_path(data_dir);
            let final_stamp = stamp_path(data_dir);

            // Atomically rename temp binary to destination.
            if let Err(e) = tokio::fs::rename(&temp_binary, &final_bin).await {
                let _ = tokio::fs::remove_file(&temp_binary).await;
                return Err(format!("Could not install Node.js binary: {e}"));
            }

            let stamp_data = format_stamp(&bin_sha256, bin_size, pin.sha256);
            if let Err(e) = tokio::fs::write(&final_stamp, stamp_data).await {
                let _ = tokio::fs::remove_file(&final_bin).await;
                return Err(format!("Could not write Node.js verification stamp: {e}"));
            }

            Ok(final_bin)
        }
        Err(err) => {
            let _ = tokio::fs::remove_file(&temp_binary).await;
            Err(err)
        }
    }
}

async fn download_extract_verify(
    url: &str,
    pin: &agentrq_desktop_core::ext_node::NodePin,
    temp_archive: &Path,
    temp_extract_dir: &Path,
    temp_binary: &Path,
    client: Option<&reqwest::Client>,
    on_progress: Option<Arc<dyn Fn(u64, u64) + Send + Sync>>,
) -> Result<(u64, String), String> {
    let local_client;
    let req_client = match client {
        Some(c) => c,
        None => {
            local_client = reqwest::Client::new();
            &local_client
        }
    };

    let response = req_client
        .get(url)
        .send()
        .await
        .map_err(|e| format!("Failed to download Node.js: {e}"))?;

    if !response.status().is_success() {
        return Err(format!(
            "Failed to download Node.js: HTTP {}",
            response.status()
        ));
    }

    let total_size = response.content_length().unwrap_or(0);
    let mut file = tokio::fs::File::create(temp_archive)
        .await
        .map_err(err_str)?;

    let mut stream = response.bytes_stream();
    let mut downloaded = 0u64;
    let mut archive_hasher = Sha256::new();

    while let Some(chunk_res) = stream.next().await {
        let chunk = chunk_res.map_err(|e| format!("Download stream interrupted: {e}"))?;
        archive_hasher.update(&chunk);
        file.write_all(&chunk).await.map_err(err_str)?;
        downloaded += chunk.len() as u64;
        if let Some(ref cb) = on_progress {
            cb(downloaded, total_size);
        }
    }

    file.flush().await.map_err(err_str)?;
    drop(file);

    let actual_archive_hash = format!("{:x}", archive_hasher.finalize());
    if actual_archive_hash != pin.sha256 {
        return Err(format!(
            "SHA-256 mismatch: expected {}, got {}",
            pin.sha256, actual_archive_hash
        ));
    }

    // Extract binary from archive or direct binary
    extract_artifact(temp_archive, temp_extract_dir, temp_binary, pin.file).await?;

    // Set read-only permissions
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(0o555);
        tokio::fs::set_permissions(temp_binary, perms)
            .await
            .map_err(err_str)?;
    }

    #[cfg(target_os = "macos")]
    {
        // Strip quarantine attribute if present so Gatekeeper permits execution.
        let _ = tokio::process::Command::new("xattr")
            .args(["-d", "com.apple.quarantine"])
            .arg(temp_binary)
            .status()
            .await;
    }

    let bin_meta = tokio::fs::metadata(temp_binary).await.map_err(err_str)?;
    let bin_bytes = tokio::fs::read(temp_binary).await.map_err(err_str)?;
    let bin_sha256 = compute_sha256(&bin_bytes);

    Ok((bin_meta.len(), bin_sha256))
}

async fn extract_artifact(
    temp_archive: &Path,
    temp_extract_dir: &Path,
    temp_binary: &Path,
    _filename: &str,
) -> Result<(), String> {
    #[cfg(target_os = "windows")]
    {
        let _ = temp_extract_dir;
        tokio::fs::copy(temp_archive, temp_binary)
            .await
            .map_err(err_str)?;
        return Ok(());
    }

    #[cfg(not(target_os = "windows"))]
    {
        // Unix archive (.tar.gz or .tar.xz)
        tokio::fs::create_dir_all(temp_extract_dir)
            .await
            .map_err(err_str)?;

        let status = tokio::process::Command::new("tar")
            .arg("-xf")
            .arg(temp_archive)
            .arg("-C")
            .arg(temp_extract_dir)
            .status()
            .await
            .map_err(err_str)?;

        if !status.success() {
            return Err(format!(
                "tar extraction failed with exit code: {:?}",
                status.code()
            ));
        }

        // Locate the extracted node binary: either in <extract_dir>/bin/node or <extract_dir>/*/bin/node
        let direct = temp_extract_dir.join("bin").join("node");
        let mut found_node = if direct.exists() { Some(direct) } else { None };

        if found_node.is_none() {
            let mut entries = tokio::fs::read_dir(temp_extract_dir)
                .await
                .map_err(err_str)?;

            while let Some(entry) = entries.next_entry().await.map_err(err_str)? {
                let candidate = entry.path().join("bin").join("node");
                if candidate.exists() {
                    found_node = Some(candidate);
                    break;
                }
            }
        }

        let source_node =
            found_node.ok_or_else(|| "Could not find node binary inside archive.".to_string())?;
        tokio::fs::copy(&source_node, temp_binary)
            .await
            .map_err(err_str)?;

        Ok(())
    }
}

static TEMP_COUNTER: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);

fn unique_temp_id() -> String {
    let nanos = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    let count = TEMP_COUNTER.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    format!("{}-{}-{}", std::process::id(), nanos, count)
}

/// Describe the current Node status for IPC.
pub fn node_status_info(data_dir: &Path) -> serde_json::Value {
    serde_json::json!({
        "installed": verify_node(data_dir).is_ok(),
        "version": NODE_VERSION,
        "size": NODE_DISPLAY_SIZE,
    })
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;

    #[test]
    fn helpers_behave_as_expected() {
        assert_eq!(err_str("disk error"), "disk error");
        assert_eq!(unsupported_platform(), "Unsupported platform for Node.js.");
    }

    #[test]
    fn node_paths_are_under_data_dir() {
        let dir = Path::new("/test/appdata");
        let bin = node_binary_path(dir);
        assert!(bin.starts_with(dir));
        let stamp = stamp_path(dir);
        assert!(stamp.starts_with(dir));
        assert!(stamp.ends_with("node.sha256"));
    }

    #[test]
    fn node_status_info_reports_uninstalled_when_missing() {
        let temp = tempfile::tempdir().unwrap();
        let status = node_status_info(temp.path());
        assert_eq!(status["installed"], false);
        assert_eq!(status["version"], NODE_VERSION);
        assert_eq!(status["size"], NODE_DISPLAY_SIZE);
    }

    #[test]
    fn verify_node_detects_missing_binary() {
        let temp = tempfile::tempdir().unwrap();
        assert!(verify_node(temp.path()).is_err());
    }

    #[test]
    fn verify_node_detects_missing_stamp() {
        let temp = tempfile::tempdir().unwrap();
        let bin = node_binary_path(temp.path());
        std::fs::create_dir_all(bin.parent().unwrap()).unwrap();
        std::fs::write(&bin, b"fake node binary").unwrap();
        let err = verify_node(temp.path()).unwrap_err();
        assert!(err.contains("verification stamp is missing"));
    }

    #[test]
    fn verify_node_detects_corrupt_stamp() {
        let temp = tempfile::tempdir().unwrap();
        let bin = node_binary_path(temp.path());
        std::fs::create_dir_all(bin.parent().unwrap()).unwrap();
        std::fs::write(&bin, b"fake node binary").unwrap();
        let stamp = stamp_path(temp.path());
        std::fs::write(&stamp, b"bad stamp").unwrap();
        let err = verify_node(temp.path()).unwrap_err();
        assert!(err.contains("Corrupted Node.js verification stamp"));
    }

    #[test]
    fn verify_node_succeeds_with_valid_stamp_and_readonly_binary() {
        let temp = tempfile::tempdir().unwrap();
        let bin = node_binary_path(temp.path());
        std::fs::create_dir_all(bin.parent().unwrap()).unwrap();
        let bytes = b"valid node content";
        std::fs::write(&bin, bytes).unwrap();

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&bin, std::fs::Permissions::from_mode(0o555)).unwrap();
        }

        let pin = current_pin().unwrap();
        let bin_hash = compute_sha256(bytes);
        let stamp_content = format_stamp(&bin_hash, bytes.len() as u64, pin.sha256);
        std::fs::write(stamp_path(temp.path()), stamp_content).unwrap();

        assert_eq!(verify_node(temp.path()).unwrap(), bin);
    }

    #[cfg(unix)]
    #[test]
    fn verify_node_refuses_writable_binary() {
        let temp = tempfile::tempdir().unwrap();
        let bin = node_binary_path(temp.path());
        std::fs::create_dir_all(bin.parent().unwrap()).unwrap();
        let bytes = b"writable node";
        std::fs::write(&bin, bytes).unwrap();

        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&bin, std::fs::Permissions::from_mode(0o755)).unwrap();

        let pin = current_pin().unwrap();
        let bin_hash = compute_sha256(bytes);
        let stamp_content = format_stamp(&bin_hash, bytes.len() as u64, pin.sha256);
        std::fs::write(stamp_path(temp.path()), stamp_content).unwrap();

        let err = verify_node(temp.path()).unwrap_err();
        assert!(err.contains("permissions are not read-only"));
    }

    #[tokio::test]
    async fn download_returns_existing_when_already_verified() {
        let temp = tempfile::tempdir().unwrap();
        let bin = node_binary_path(temp.path());
        std::fs::create_dir_all(bin.parent().unwrap()).unwrap();
        let bytes = b"valid node content";
        std::fs::write(&bin, bytes).unwrap();

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&bin, std::fs::Permissions::from_mode(0o555)).unwrap();
        }

        let pin = current_pin().unwrap();
        let bin_hash = compute_sha256(bytes);
        let stamp_content = format_stamp(&bin_hash, bytes.len() as u64, pin.sha256);
        std::fs::write(stamp_path(temp.path()), stamp_content).unwrap();

        // No network called when already verified
        let res =
            download_and_install_node(temp.path(), None, Some("http://invalid.test"), None, None)
                .await;
        assert_eq!(res.unwrap(), bin);
    }

    #[tokio::test]
    async fn download_handles_no_network() {
        let temp = tempfile::tempdir().unwrap();
        let res =
            download_and_install_node(temp.path(), None, Some("http://127.0.0.1:1"), None, None)
                .await;
        assert!(res.is_err());
        let err = res.unwrap_err();
        assert!(err.contains("Failed to download Node.js"));
        // Nothing installed
        assert!(!node_binary_path(temp.path()).exists());
        assert!(!stamp_path(temp.path()).exists());
    }

    use crate::test_server::{reply, send, Step, TestServer};

    pub(crate) fn make_mock_tarball(node_content: &[u8]) -> (Vec<u8>, String) {
        let work = tempfile::tempdir().unwrap();
        let pin = current_pin().unwrap();
        let root_dir = "node-v24.22.1-test".to_string();
        let sub = work.path().join(&root_dir).join("bin");
        std::fs::create_dir_all(&sub).unwrap();
        let node_mock = sub.join("node");
        std::fs::write(&node_mock, node_content).unwrap();

        let archive_path = work.path().join(pin.file);
        let status = std::process::Command::new("tar")
            .arg("-cf")
            .arg(&archive_path)
            .arg("-C")
            .arg(work.path())
            .arg(&root_dir)
            .status()
            .unwrap();
        assert!(status.success());
        let archive_bytes = std::fs::read(&archive_path).unwrap();
        let archive_hash = compute_sha256(&archive_bytes);
        (archive_bytes, archive_hash)
    }

    #[tokio::test]
    async fn download_handles_http_error() {
        let server = TestServer::start(|_seen| reply("500 Internal Server Error", &[], "")).await;
        let temp = tempfile::tempdir().unwrap();
        let res = download_and_install_node(temp.path(), None, Some(&server.url), None, None).await;
        assert!(res.is_err());
        let err = res.unwrap_err();
        assert!(err.contains("HTTP 500"));
    }

    #[tokio::test]
    async fn download_detects_hash_mismatch() {
        let server =
            TestServer::start(|_seen| reply("200 OK", &[], "corrupted archive with bad hash"))
                .await;
        let temp = tempfile::tempdir().unwrap();
        let res = download_and_install_node(temp.path(), None, Some(&server.url), None, None).await;
        assert!(res.is_err());
        let err = res.unwrap_err();
        assert!(err.contains("SHA-256 mismatch"));
        // Temp files cleaned up, nothing installed
        assert!(!node_binary_path(temp.path()).exists());
        assert!(!stamp_path(temp.path()).exists());
    }

    #[tokio::test]
    async fn download_handles_interrupted_stream() {
        let server = TestServer::start(|_seen| {
            vec![send(
                "HTTP/1.1 200 OK\r\nContent-Length: 10000\r\nConnection: close\r\n\r\n1234567890",
            )]
        })
        .await;
        let temp = tempfile::tempdir().unwrap();
        let res = download_and_install_node(temp.path(), None, Some(&server.url), None, None).await;
        assert!(res.is_err());
        assert!(!node_binary_path(temp.path()).exists());
        assert!(!stamp_path(temp.path()).exists());
    }

    #[tokio::test]
    async fn download_with_custom_client_and_default_url() {
        let temp = tempfile::tempdir().unwrap();
        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_millis(1))
            .build()
            .unwrap();
        let res = download_and_install_node(temp.path(), Some(&client), None, None, None).await;
        assert!(res.is_err());
    }

    #[tokio::test]
    async fn download_handles_rename_failure() {
        let temp = tempfile::tempdir().unwrap();
        let final_bin = node_binary_path(temp.path());
        std::fs::create_dir_all(final_bin.join("blocker")).unwrap();

        let (archive_bytes, archive_hash) = make_mock_tarball(b"mock node script");
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

        let pin = current_pin().unwrap();
        let mock_pin = agentrq_desktop_core::ext_node::NodePin {
            file: pin.file,
            sha256: Box::leak(archive_hash.into_boxed_str()),
        };

        let res =
            download_and_install_node(temp.path(), None, Some(&server.url), None, Some(&mock_pin))
                .await;
        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .contains("Could not install Node.js binary"));
    }

    #[tokio::test]
    async fn download_handles_stamp_write_failure() {
        let temp = tempfile::tempdir().unwrap();
        let final_stamp = stamp_path(temp.path());
        std::fs::create_dir_all(&final_stamp).unwrap();

        let (archive_bytes, archive_hash) = make_mock_tarball(b"mock node script");
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

        let pin = current_pin().unwrap();
        let mock_pin = agentrq_desktop_core::ext_node::NodePin {
            file: pin.file,
            sha256: Box::leak(archive_hash.into_boxed_str()),
        };

        let res =
            download_and_install_node(temp.path(), None, Some(&server.url), None, Some(&mock_pin))
                .await;
        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .contains("Could not write Node.js verification stamp"));
        assert!(!node_binary_path(temp.path()).exists());
    }

    #[tokio::test]
    async fn download_handles_archive_missing_node_binary() {
        let temp = tempfile::tempdir().unwrap();
        let work = tempfile::tempdir().unwrap();
        let sub = work.path().join("other");
        std::fs::create_dir_all(&sub).unwrap();
        std::fs::write(sub.join("file.txt"), b"other content").unwrap();

        let pin = current_pin().unwrap();
        let archive_path = work.path().join(pin.file);
        let status = std::process::Command::new("tar")
            .arg("-cf")
            .arg(&archive_path)
            .arg("-C")
            .arg(work.path())
            .arg("other")
            .status()
            .unwrap();
        assert!(status.success());
        let archive_bytes = std::fs::read(&archive_path).unwrap();
        let archive_hash = compute_sha256(&archive_bytes);

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

        let mock_pin = agentrq_desktop_core::ext_node::NodePin {
            file: pin.file,
            sha256: Box::leak(archive_hash.into_boxed_str()),
        };

        let res =
            download_and_install_node(temp.path(), None, Some(&server.url), None, Some(&mock_pin))
                .await;
        assert!(res.is_err());
        assert!(res.unwrap_err().contains("Could not find node binary"));
    }

    #[tokio::test]
    async fn extract_handles_invalid_tar() {
        let temp = tempfile::tempdir().unwrap();
        let corrupt_tar = temp.path().join("corrupt.tar");
        std::fs::write(&corrupt_tar, b"not a tar file").unwrap();
        let extract_dir = temp.path().join("extract");
        let bin = temp.path().join("node");
        let res = extract_artifact(&corrupt_tar, &extract_dir, &bin, "node.tar.xz").await;
        assert!(res.is_err());
        assert!(res.unwrap_err().contains("tar extraction failed"));
    }

    #[tokio::test]
    async fn extract_finds_direct_bin_node() {
        let temp = tempfile::tempdir().unwrap();
        let work = tempfile::tempdir().unwrap();
        let sub = work.path().join("bin");
        std::fs::create_dir_all(&sub).unwrap();
        let node_mock = sub.join("node");
        std::fs::write(&node_mock, b"mock node script").unwrap();

        let archive_path = temp.path().join("direct.tar");
        let status = std::process::Command::new("tar")
            .arg("-cf")
            .arg(&archive_path)
            .arg("-C")
            .arg(work.path())
            .arg("bin")
            .status()
            .unwrap();
        assert!(status.success());

        let extract_dir = temp.path().join("extract");
        let bin = temp.path().join("node_out");
        extract_artifact(&archive_path, &extract_dir, &bin, "node.tar.gz")
            .await
            .unwrap();
        assert!(bin.exists());
    }

    #[tokio::test]
    async fn download_extracts_and_installs_successfully() {
        let temp = tempfile::tempdir().unwrap();
        let (archive_bytes, archive_hash) = make_mock_tarball(b"mock node script");

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

        let pin = current_pin().unwrap();
        let mock_pin = agentrq_desktop_core::ext_node::NodePin {
            file: pin.file,
            sha256: Box::leak(archive_hash.into_boxed_str()),
        };

        let progress_recorded = Arc::new(std::sync::atomic::AtomicBool::new(false));
        let p_rec = progress_recorded.clone();
        let on_progress: Arc<dyn Fn(u64, u64) + Send + Sync> = Arc::new(move |_d, _t| {
            p_rec.store(true, std::sync::atomic::Ordering::Relaxed);
        });

        let bin = download_and_install_node(
            temp.path(),
            None,
            Some(&server.url),
            Some(on_progress),
            Some(&mock_pin),
        )
        .await
        .unwrap();

        assert!(progress_recorded.load(std::sync::atomic::Ordering::Relaxed));
        assert!(bin.exists());
        assert!(stamp_path(temp.path()).exists());
        let content = std::fs::read_to_string(stamp_path(temp.path())).unwrap();
        let stamp = parse_stamp(&content).unwrap();
        let meta = std::fs::metadata(&bin).unwrap();
        let bytes = std::fs::read(&bin).unwrap();
        assert!(
            validate_stamp(&stamp, mock_pin.sha256, meta.len(), &compute_sha256(&bytes)).is_ok()
        );

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&bin).unwrap().permissions().mode();
            assert_eq!(mode & 0o222, 0); // read only
        }
    }
}
