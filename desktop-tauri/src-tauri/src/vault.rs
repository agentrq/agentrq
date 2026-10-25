// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The key the cookie jars are encrypted with, and the encryption.
//!
//! The key lives in the OS keychain, so a copy of the profile folder is not a
//! copy of the session. Where there is no keychain (a Linux box with no Secret
//! Service) it lives in a file only this user can read, beside the jars: the
//! same fallback Chromium makes there, and no weaker than the jar it protects.

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

/// The OS keychain's entry for the key. `Err` means there is no keychain.
pub trait Keychain {
    fn get(&self) -> Result<Option<Vec<u8>>, String>;
    fn set(&self, secret: &[u8]) -> Result<(), String>;
}

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

/// A key nobody stored: what is sealed with it lasts until the app quits.
pub fn random_key() -> [u8; KEY_LEN] {
    random()
}

fn random<const N: usize>() -> [u8; N] {
    let mut bytes = [0; N];
    getrandom::fill(&mut bytes).expect("the OS random source is available");
    bytes
}

/// The key: the keychain's, a new one stored there, or, with no keychain, the
/// one in `fallback` (created on first use, readable by this user only).
pub fn load_key(keychain: &dyn Keychain, fallback: &Path) -> io::Result<[u8; KEY_LEN]> {
    match keychain.get() {
        Ok(Some(found)) => {
            if let Ok(key) = <[u8; KEY_LEN]>::try_from(found.as_slice()) {
                return Ok(key);
            }
        }
        Ok(None) => {}
        Err(_) => return file_key(fallback),
    }
    let key = random();
    match keychain.set(&key) {
        Ok(()) => Ok(key),
        Err(_) => file_key(fallback),
    }
}

fn file_key(path: &Path) -> io::Result<[u8; KEY_LEN]> {
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
    use std::cell::RefCell;

    /// A keychain holding at most one secret, which can be made to fail.
    #[derive(Default)]
    struct Fake {
        secret: RefCell<Option<Vec<u8>>>,
        no_keychain: bool,
        refuse_set: bool,
    }

    impl Keychain for Fake {
        fn get(&self) -> Result<Option<Vec<u8>>, String> {
            if self.no_keychain {
                return Err("no keychain".into());
            }
            Ok(self.secret.borrow().clone())
        }
        fn set(&self, secret: &[u8]) -> Result<(), String> {
            if self.refuse_set {
                return Err("locked".into());
            }
            *self.secret.borrow_mut() = Some(secret.to_vec());
            Ok(())
        }
    }

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
    fn keeps_the_key_in_the_keychain() {
        let dir = tempfile::tempdir().unwrap();
        let fallback = dir.path().join("key");
        let keychain = Fake::default();

        let first = load_key(&keychain, &fallback).unwrap();
        assert_eq!(keychain.secret.borrow().as_deref(), Some(&first[..]));
        assert_eq!(load_key(&keychain, &fallback).unwrap(), first);
        assert!(!fallback.exists(), "no key file while there is a keychain");
    }

    #[test]
    fn replaces_a_keychain_entry_that_is_not_a_key() {
        let dir = tempfile::tempdir().unwrap();
        let keychain = Fake::default();
        *keychain.secret.borrow_mut() = Some(b"short".to_vec());

        let key = load_key(&keychain, &dir.path().join("key")).unwrap();
        assert_eq!(keychain.secret.borrow().as_deref(), Some(&key[..]));
    }

    #[test]
    fn falls_back_to_a_private_file_without_a_keychain() {
        let dir = tempfile::tempdir().unwrap();
        let fallback = dir.path().join("nested").join("key");
        let keychain = Fake {
            no_keychain: true,
            ..Fake::default()
        };

        let key = load_key(&keychain, &fallback).unwrap();
        assert_eq!(fs::read(&fallback).unwrap(), key);
        assert_eq!(load_key(&keychain, &fallback).unwrap(), key);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = fs::metadata(&fallback).unwrap().permissions().mode();
            assert_eq!(mode & 0o777, 0o600);
        }
    }

    #[test]
    fn falls_back_to_the_file_when_the_keychain_will_not_store_it() {
        let dir = tempfile::tempdir().unwrap();
        let fallback = dir.path().join("key");
        let keychain = Fake {
            refuse_set: true,
            ..Fake::default()
        };

        let key = load_key(&keychain, &fallback).unwrap();
        assert_eq!(fs::read(&fallback).unwrap(), key);
    }

    #[test]
    fn reports_a_key_file_it_cannot_write() {
        let dir = tempfile::tempdir().unwrap();
        let blocker = dir.path().join("file");
        fs::write(&blocker, b"x").unwrap();
        let keychain = Fake {
            no_keychain: true,
            ..Fake::default()
        };
        assert!(load_key(&keychain, &blocker.join("key")).is_err());
    }
}
