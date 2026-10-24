// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The file install.sh writes to, so the updater can read its progress back.
//!
//! A file, never a pipe: the installer quits this app mid-install, and a pipe
//! with no reader would kill it there. The installer gets the file as its
//! stdout and stderr, and it is read back by path.

use std::fs::File;
use std::io;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::sync::Mutex;

pub const INSTALL_LOG_FILENAME: &str = "agentrq-update.log";

/// This OS's shell and the flag that hands it a script, so the tests that
/// spawn a real installer run on Windows too.
#[cfg(all(test, windows))]
pub(crate) const TEST_SHELL: (&str, &str) = ("cmd", "/C");
#[cfg(all(test, not(windows)))]
pub(crate) const TEST_SHELL: (&str, &str) = ("/bin/sh", "-c");

/// Where an installer writes, and how far it got.
pub trait InstallLog: Send + Sync {
    /// The installer's stdout and stderr: both end up in the same file.
    fn stdio(&self) -> io::Result<(Stdio, Stdio)>;
    /// Everything written so far, or empty when it cannot be read.
    fn read(&self) -> String;
    /// Idempotent: the updater closes on every way out.
    fn close(&self);
}

pub struct FileInstallLog {
    path: PathBuf,
    file: Mutex<Option<File>>,
}

/// Create (or truncate) the log in `dir`.
pub fn create_install_log(dir: &Path) -> io::Result<FileInstallLog> {
    let path = dir.join(INSTALL_LOG_FILENAME);
    let file = File::create(&path)?;
    Ok(FileInstallLog {
        path,
        file: Mutex::new(Some(file)),
    })
}

impl FileInstallLog {
    pub fn path(&self) -> &Path {
        &self.path
    }
}

impl InstallLog for FileInstallLog {
    fn stdio(&self) -> io::Result<(Stdio, Stdio)> {
        let guard = self.file.lock().unwrap();
        let file = guard
            .as_ref()
            .ok_or_else(|| io::Error::other("the install log is closed"))?;
        // Duplicated descriptors share one offset, so the two streams interleave
        // in the file rather than overwriting each other.
        Ok((
            Stdio::from(file.try_clone()?),
            Stdio::from(file.try_clone()?),
        ))
    }

    fn read(&self) -> String {
        // Lossy, as Node's `readFileSync(path, 'utf8')`: one stray byte must not
        // read as an empty log.
        std::fs::read(&self.path)
            .map(|bytes| String::from_utf8_lossy(&bytes).into_owned())
            .unwrap_or_default()
    }

    fn close(&self) {
        self.file.lock().unwrap().take();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("agentrq-{name}-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn gives_the_installer_a_descriptor_and_reads_back_what_it_wrote() {
        let dir = temp_dir("install-log-a");
        let log = create_install_log(&dir).unwrap();

        assert_eq!(log.path(), dir.join("agentrq-update.log"));
        assert_eq!(log.read(), "");

        // Both streams land in the same file, in the order they were written.
        // The script means the same to sh and cmd; `&&` has no space before
        // it, which cmd's echo would print.
        let (stdout, stderr) = log.stdio().unwrap();
        let (shell, flag) = TEST_SHELL;
        std::process::Command::new(shell)
            .args([flag, "echo Downloading x.dmg...&& 1>&2 echo error: nope"])
            .stdout(stdout)
            .stderr(stderr)
            .status()
            .unwrap();
        // cmd ends its lines with CRLF.
        assert_eq!(
            log.read().replace("\r\n", "\n"),
            "Downloading x.dmg...\nerror: nope\n"
        );

        log.close();
        // Closing twice is harmless: the updater closes on every way out.
        log.close();
        assert!(log.stdio().is_err());
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn reads_as_empty_once_the_file_has_gone() {
        let dir = temp_dir("install-log-b");
        let log = create_install_log(&dir).unwrap();
        // Closed first: Windows will not remove a folder holding an open file.
        log.close();
        std::fs::remove_dir_all(&dir).unwrap();

        assert_eq!(log.read(), "");
    }

    #[test]
    fn reads_a_byte_that_is_not_utf8_as_a_replacement_character() {
        let dir = temp_dir("install-log-d");
        let log = create_install_log(&dir).unwrap();
        std::fs::write(log.path(), b"installed to /home/jos\xE9/Applications\n").unwrap();

        assert_eq!(log.read(), "installed to /home/jos\u{fffd}/Applications\n");
        log.close();
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn cannot_be_created_where_there_is_no_folder() {
        let dir = temp_dir("install-log-c");
        std::fs::remove_dir_all(&dir).unwrap();
        assert!(create_install_log(&dir).is_err());
    }
}
