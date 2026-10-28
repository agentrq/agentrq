// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The keys the cookie jars and the extension secrets are encrypted with, and
//! the encryption.
//!
//! Each key is random and lives in a file only this user can read, beside what
//! it protects, as Chromium keeps its key on a Linux box with no keyring. The
//! OS keychain is never asked: on macOS an unsigned or re-signed build makes it
//! prompt for the login password, and a refusal there signed every profile out.

use std::fs;
use std::io::{self, Write};
use std::path::Path;

use chacha20poly1305::aead::{Aead, KeyInit};
use chacha20poly1305::{XChaCha20Poly1305, XNonce};

/// Bytes in a key.
pub const KEY_LEN: usize = 32;
const NONCE_LEN: usize = 24;
/// What a sealed file starts with, so a different format is never fed to the
/// cipher as if it were this one.
const MAGIC: &[u8] = b"ARQV1";

/// Seals and opens what is written to disk.
#[derive(Clone)]
pub struct Vault {
    cipher: XChaCha20Poly1305,
}

impl Vault {
    pub fn new(key: &[u8; KEY_LEN]) -> Self {
        Self {
            cipher: XChaCha20Poly1305::new(&(*key).into()),
        }
    }

    /// `MAGIC`, a fresh nonce, then the ciphertext and its tag.
    pub fn seal(&self, plain: &[u8]) -> Vec<u8> {
        let nonce: [u8; NONCE_LEN] = random();
        let sealed = self
            .cipher
            .encrypt(&XNonce::from(nonce), plain)
            .expect("an in-memory buffer always encrypts");
        [MAGIC, &nonce, &sealed].concat()
    }

    /// `None` for anything this vault did not seal, or that was changed since.
    pub fn open(&self, sealed: &[u8]) -> Option<Vec<u8>> {
        let rest = sealed.strip_prefix(MAGIC)?;
        let (nonce, body) = rest.split_at_checked(NONCE_LEN)?;
        let nonce: [u8; NONCE_LEN] = nonce.try_into().ok()?;
        self.cipher.decrypt(&XNonce::from(nonce), body).ok()
    }
}

/// A key nobody stored, for a test's vault.
#[cfg(test)]
pub fn random_key() -> [u8; KEY_LEN] {
    random()
}

pub(crate) fn random<const N: usize>() -> [u8; N] {
    let mut bytes = [0; N];
    getrandom::fill(&mut bytes).expect("the OS random source is available");
    bytes
}

/// The key in `path`, or a new one written there, readable by this user only.
/// A file that is not a key is replaced: what it sealed could not be opened
/// anyway. `Err` when it cannot be written; the caller then keeps what it
/// would have sealed in memory.
pub fn load_key(path: &Path) -> io::Result<[u8; KEY_LEN]> {
    if let Ok(key) = <[u8; KEY_LEN]>::try_from(fs::read(path).unwrap_or_default().as_slice()) {
        return Ok(key);
    }
    let key = random();
    write_private(path, &key)?;
    Ok(key)
}

/// Writes a file only this user can read, whole or not at all: a sibling is
/// written first and renamed over the target.
pub fn write_private(path: &Path, bytes: &[u8]) -> io::Result<()> {
    fs::create_dir_all(path.parent().unwrap_or(Path::new("")))?;
    let mut part = path.as_os_str().to_owned();
    part.push(".part");
    let mut options = fs::OpenOptions::new();
    options.write(true).create(true).truncate(true);
    #[cfg(unix)]
    std::os::unix::fs::OpenOptionsExt::mode(&mut options, 0o600);
    let mut file = options.open(&part)?;
    file.write_all(bytes)?;
    file.sync_all()?;
    drop(file);
    fs::rename(&part, path)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn opens_what_it_sealed_and_nothing_else() {
        let vault = Vault::new(&random_key());
        let sealed = vault.seal(b"at=1");
        assert!(sealed.starts_with(MAGIC));
        assert!(!sealed.windows(4).any(|w| w == b"at=1"));
        assert_eq!(vault.open(&sealed).as_deref(), Some(&b"at=1"[..]));

        // A fresh nonce every time.
        assert_ne!(vault.seal(b"at=1"), sealed);

        let mut changed = sealed.clone();
        *changed.last_mut().unwrap() ^= 1;
        assert_eq!(vault.open(&changed), None);
        assert_eq!(Vault::new(&random_key()).open(&sealed), None);
        assert_eq!(vault.open(b"plain json"), None);
        assert_eq!(vault.open(b"ARQV1short"), None);
    }

    #[test]
    fn makes_a_private_key_file_once_and_keeps_it() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("nested").join("key");

        let key = load_key(&path).unwrap();
        assert_eq!(fs::read(&path).unwrap(), key);
        assert_eq!(load_key(&path).unwrap(), key);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = fs::metadata(&path).unwrap().permissions().mode();
            assert_eq!(mode & 0o777, 0o600);
        }
    }

    #[test]
    fn replaces_a_file_that_is_not_a_key() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("key");
        fs::write(&path, b"short").unwrap();

        let key = load_key(&path).unwrap();
        assert_eq!(fs::read(&path).unwrap(), key);
    }

    #[test]
    fn reports_a_key_file_it_cannot_write() {
        let dir = tempfile::tempdir().unwrap();
        let blocker = dir.path().join("file");
        fs::write(&blocker, b"x").unwrap();
        assert!(load_key(&blocker.join("key")).is_err());
    }
}
