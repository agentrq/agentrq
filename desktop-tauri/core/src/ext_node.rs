// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Logic for downloading and verifying the pinned Node.js runtime for the extension host.

use sha2::{Digest, Sha256};

pub const NODE_VERSION: &str = "v24.22.1";
pub const NODE_DISPLAY_SIZE: &str = "about 40 MB";

/// A pinned Node build for a specific target platform.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NodePin {
    pub file: &'static str,
    pub sha256: &'static str,
}

/// A parsed verification stamp stored beside the installed binary.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Stamp {
    pub binary_sha256: String,
    pub binary_size: u64,
    pub archive_sha256: String,
}

/// Find the pinned Node build for the given OS and architecture.
pub fn target_pin(os: &str, arch: &str) -> Option<NodePin> {
    match (os, arch) {
        ("macos", "aarch64") => Some(NodePin {
            file: "node-v24.22.1-darwin-arm64.tar.gz",
            sha256: "89c682593d76cd44233fa53b1563264e7de9cbb189cad169135c5b4fadfbc0a7",
        }),
        ("macos", "x86_64") => Some(NodePin {
            file: "node-v24.22.1-darwin-x64.tar.gz",
            sha256: "d132557e7ee2b85af08b0c8c34338e65c6430f2238d01f727728ef42dc83b035",
        }),
        ("linux", "aarch64") => Some(NodePin {
            file: "node-v24.22.1-linux-arm64.tar.xz",
            sha256: "dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e",
        }),
        ("linux", "x86_64") => Some(NodePin {
            file: "node-v24.22.1-linux-x64.tar.xz",
            sha256: "6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6",
        }),
        ("windows", "x86_64") => Some(NodePin {
            file: "win-x64/node.exe",
            sha256: "d702daa815eabbff763c7d3808e235cf031cf2ca70211b56847bc72f51e240e5",
        }),
        ("windows", "aarch64") => Some(NodePin {
            file: "win-arm64/node.exe",
            sha256: "8bea91dc437fe01e7d92ef0efc6c9eaf654369aaba9b2d3e9841552ea7b95a00",
        }),
        _ => None,
    }
}

/// The pin for the currently running target platform.
pub fn current_pin() -> Option<NodePin> {
    target_pin(std::env::consts::OS, std::env::consts::ARCH)
}

/// The relative path of the Node binary within the app data dir.
pub fn node_relative_path(os: &str) -> &'static str {
    if os == "windows" {
        "bin/node.exe"
    } else {
        "bin/node"
    }
}

/// The official nodejs.org download URL for a pinned file.
pub fn download_url(file: &str) -> String {
    format!("https://nodejs.org/dist/{NODE_VERSION}/{file}")
}

/// Compute the lowercase hex SHA-256 digest of `bytes`.
pub fn compute_sha256(bytes: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    format!("{:x}", hasher.finalize())
}

/// Format a verification stamp string from binary hash, size, and archive hash.
pub fn format_stamp(bin_sha256: &str, bin_size: u64, archive_sha256: &str) -> String {
    format!("{bin_sha256} {bin_size} {archive_sha256}\n")
}

/// Parse a verification stamp string.
pub fn parse_stamp(content: &str) -> Option<Stamp> {
    let mut parts = content.split_whitespace();
    let binary_sha256 = parts.next()?.to_string();
    let binary_size = parts.next()?.parse().ok()?;
    let archive_sha256 = parts.next()?.to_string();
    if binary_sha256.len() != 64 || archive_sha256.len() != 64 {
        return None;
    }
    Some(Stamp {
        binary_sha256,
        binary_size,
        archive_sha256,
    })
}

/// Validate an installed binary against its stamp and the currently expected archive hash.
pub fn validate_stamp(
    stamp: &Stamp,
    expected_archive_sha256: &str,
    actual_size: u64,
    actual_bin_sha256: &str,
) -> Result<(), &'static str> {
    if stamp.archive_sha256 != expected_archive_sha256 {
        return Err("The installed Node archive hash does not match current pinned version.");
    }
    if stamp.binary_size != actual_size {
        return Err("The installed Node binary size does not match verification stamp.");
    }
    if stamp.binary_sha256 != actual_bin_sha256 {
        return Err("The installed Node binary hash does not match verification stamp.");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_all_supported_targets() {
        assert!(target_pin("macos", "aarch64").is_some());
        assert!(target_pin("macos", "x86_64").is_some());
        assert!(target_pin("linux", "aarch64").is_some());
        assert!(target_pin("linux", "x86_64").is_some());
        assert!(target_pin("windows", "x86_64").is_some());
        assert!(target_pin("windows", "aarch64").is_some());
        assert!(target_pin("freebsd", "x86_64").is_none());
        assert!(target_pin("linux", "riscv64").is_none());
    }

    #[test]
    fn current_pin_resolves_for_host() {
        assert!(current_pin().is_some());
    }

    #[test]
    fn relative_path_matches_os() {
        assert_eq!(node_relative_path("windows"), "bin/node.exe");
        assert_eq!(node_relative_path("linux"), "bin/node");
        assert_eq!(node_relative_path("macos"), "bin/node");
    }

    #[test]
    fn download_url_formats_expected_path() {
        assert_eq!(
            download_url("win-x64/node.exe"),
            "https://nodejs.org/dist/v24.22.1/win-x64/node.exe"
        );
    }

    #[test]
    fn computes_sha256() {
        assert_eq!(
            compute_sha256(b"hello world"),
            "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
        );
    }

    #[test]
    fn stamp_round_trip() {
        let bin_hash = "a".repeat(64);
        let arc_hash = "b".repeat(64);
        let formatted = format_stamp(&bin_hash, 12345, &arc_hash);
        let parsed = parse_stamp(&formatted).expect("parsed stamp");
        assert_eq!(
            parsed,
            Stamp {
                binary_sha256: bin_hash.clone(),
                binary_size: 12345,
                archive_sha256: arc_hash.clone(),
            }
        );
        assert!(validate_stamp(&parsed, &arc_hash, 12345, &bin_hash).is_ok());
    }

    #[test]
    fn parse_stamp_refuses_malformed() {
        assert!(parse_stamp("").is_none());
        assert!(parse_stamp("not enough parts").is_none());
        assert!(parse_stamp("short 123 short").is_none());
        let good_hash = "a".repeat(64);
        assert!(parse_stamp(&format!("{good_hash} notanumber {good_hash}")).is_none());
        assert!(parse_stamp(&format!("short 123 {good_hash}")).is_none());
        assert!(parse_stamp(&format!("{good_hash} 123 short")).is_none());
    }

    #[test]
    fn validate_stamp_detects_mismatches() {
        let bin_hash = "a".repeat(64);
        let arc_hash = "b".repeat(64);
        let stamp = Stamp {
            binary_sha256: bin_hash.clone(),
            binary_size: 100,
            archive_sha256: arc_hash.clone(),
        };

        // Archive hash mismatch
        assert!(validate_stamp(&stamp, &"c".repeat(64), 100, &bin_hash).is_err());
        // Binary size mismatch
        assert!(validate_stamp(&stamp, &arc_hash, 101, &bin_hash).is_err());
        // Binary hash mismatch
        assert!(validate_stamp(&stamp, &arc_hash, 100, &"c".repeat(64)).is_err());
    }
}
