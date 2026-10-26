// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The Node.js the extension host runs on: which build, where it comes from,
//! and how its binary is taken out of the archive and checked.
//!
//! Node is not bundled. The Extensions page downloads it once, when the person
//! says so. Each pin names the archive and the binary inside it with both
//! SHA-256s and sizes, so a binary on disk is checked against the build this
//! app pins, not against a record kept beside it in the same writable folder.

use std::io::{self, Read, Seek, Write};

use sha2::{Digest, Sha256};

pub const NODE_VERSION: &str = "v24.22.1";

/// How an archive is packed.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Packing {
    TarXz,
    Zip,
}

/// A pinned Node build: the archive nodejs.org serves, and its binary.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct NodePin {
    pub archive: &'static str,
    pub archive_sha256: &'static str,
    pub archive_size: u64,
    pub packing: Packing,
    /// The binary's path inside the archive.
    pub member: &'static str,
    pub binary_sha256: &'static str,
    pub binary_size: u64,
}

/// Every pin, by `std::env::consts` OS and architecture. The archive hashes
/// are nodejs.org's `SHASUMS256.txt`; the binary ones were taken from those
/// archives.
static PINS: [(&str, &str, NodePin); 6] = [
    (
        "macos",
        "aarch64",
        NodePin {
            archive: "node-v24.22.1-darwin-arm64.tar.xz",
            archive_sha256: "654ba79de6155647e22321932b45f2a1ccb705976cc337c9b8e278beb085a628",
            archive_size: 27_518_756,
            packing: Packing::TarXz,
            member: "node-v24.22.1-darwin-arm64/bin/node",
            binary_sha256: "6f32069480ec14d52b6d7b1f3c7124cfbbc46203abf5ddf5c65b127435f3f329",
            binary_size: 122_545_856,
        },
    ),
    (
        "macos",
        "x86_64",
        NodePin {
            archive: "node-v24.22.1-darwin-x64.tar.xz",
            archive_sha256: "65e450f90a6eaacd5000f611861c4ce65fadc87f813795dc4ed0c0e5f2fca2d4",
            archive_size: 29_283_404,
            packing: Packing::TarXz,
            member: "node-v24.22.1-darwin-x64/bin/node",
            binary_sha256: "cc6f41f72e3eb4460154952e56a35afdd96b6995cea51cd25e637355a2955a51",
            binary_size: 125_715_808,
        },
    ),
    (
        "linux",
        "aarch64",
        NodePin {
            archive: "node-v24.22.1-linux-arm64.tar.xz",
            archive_sha256: "dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e",
            archive_size: 30_978_868,
            packing: Packing::TarXz,
            member: "node-v24.22.1-linux-arm64/bin/node",
            binary_sha256: "c8c5721679d8d1a09e9987e345d5f9a64899bb85f7a384783ba8a93e6e62348a",
            binary_size: 123_377_224,
        },
    ),
    (
        "linux",
        "x86_64",
        NodePin {
            archive: "node-v24.22.1-linux-x64.tar.xz",
            archive_sha256: "6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6",
            archive_size: 32_026_916,
            packing: Packing::TarXz,
            member: "node-v24.22.1-linux-x64/bin/node",
            binary_sha256: "e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb",
            binary_size: 126_879_696,
        },
    ),
    (
        "windows",
        "x86_64",
        NodePin {
            archive: "node-v24.22.1-win-x64.zip",
            archive_sha256: "d2046d28c2c3dcd211ac5145c0bf30e2047ef9dfb306693c8c6e2eccd8b95fa5",
            archive_size: 37_748_064,
            packing: Packing::Zip,
            member: "node-v24.22.1-win-x64/node.exe",
            binary_sha256: "d702daa815eabbff763c7d3808e235cf031cf2ca70211b56847bc72f51e240e5",
            binary_size: 93_915_464,
        },
    ),
    (
        "windows",
        "aarch64",
        NodePin {
            archive: "node-v24.22.1-win-arm64.zip",
            archive_sha256: "a0c807319fede24c4a0d8f84564c82a619bd313b16b10c007c383c2609e99171",
            archive_size: 33_786_263,
            packing: Packing::Zip,
            member: "node-v24.22.1-win-arm64/node.exe",
            binary_sha256: "8bea91dc437fe01e7d92ef0efc6c9eaf654369aaba9b2d3e9841552ea7b95a00",
            binary_size: 82_150_728,
        },
    ),
];

/// The binary's name in the app's `bin` folder.
#[cfg(windows)]
pub const BINARY_NAME: &str = "node.exe";
#[cfg(not(windows))]
pub const BINARY_NAME: &str = "node";

/// The pinned build for an OS and architecture, as `std::env::consts` names them.
pub fn target_pin(os: &str, arch: &str) -> Option<&'static NodePin> {
    PINS.iter()
        .find(|(o, a, _)| *o == os && *a == arch)
        .map(|(_, _, pin)| pin)
}

/// The pinned build for this machine.
pub fn current_pin() -> Option<&'static NodePin> {
    target_pin(std::env::consts::OS, std::env::consts::ARCH)
}

/// Where nodejs.org serves a pinned archive.
pub fn download_url(pin: &NodePin) -> String {
    format!("https://nodejs.org/dist/{NODE_VERSION}/{}", pin.archive)
}

/// Why an extension cannot run or be installed yet.
pub fn needs_node() -> String {
    format!("Extensions need Node.js {NODE_VERSION}. Download it from the Extensions page.")
}

/// A temporary file a download keeps in `bin` until it is renamed or removed.
/// One left by a quit mid-download is swept up later.
pub fn is_leftover(name: &str) -> bool {
    name.starts_with(".node-") && name.ends_with(".tmp")
}

/// How far a download is, in whole percent: never above 100, and 0 while
/// the size is unknown.
pub fn percent(done: u64, total: u64) -> u32 {
    match total {
        0 => 0,
        total => (done.min(total) * 100 / total) as u32,
    }
}

/// The progress worth telling: only a move of a whole percent, so a download
/// sends at most 101 events however small its chunks.
#[derive(Debug, Default)]
pub struct Steps {
    last: Option<u32>,
}

impl Steps {
    pub fn advance(&mut self, done: u64, total: u64) -> Option<u32> {
        let now = percent(done, total);
        (self.last != Some(now)).then(|| {
            self.last = Some(now);
            now
        })
    }
}

/// A SHA-256, written out as lowercase hex.
#[derive(Default)]
pub struct Hasher(Sha256);

impl Hasher {
    pub fn update(&mut self, bytes: &[u8]) {
        self.0.update(bytes);
    }

    pub fn finish(self) -> String {
        self.0
            .finalize()
            .iter()
            .map(|byte| format!("{byte:02x}"))
            .collect()
    }
}

/// A writer that hashes and counts what passes through it.
pub struct Hashing<'a> {
    out: &'a mut dyn Write,
    hasher: Hasher,
    written: u64,
}

impl<'a> Hashing<'a> {
    pub fn new(out: &'a mut dyn Write) -> Self {
        Self {
            out,
            hasher: Hasher::default(),
            written: 0,
        }
    }

    /// The size and SHA-256 of everything written.
    pub fn finish(self) -> (u64, String) {
        (self.written, self.hasher.finish())
    }
}

impl Write for Hashing<'_> {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        let written = self.out.write(buf)?;
        self.hasher.update(&buf[..written]);
        self.written += written as u64;
        Ok(written)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.out.flush()
    }
}

/// The size and SHA-256 of all of `input`.
pub fn sha256_of(input: &mut dyn Read) -> io::Result<(u64, String)> {
    let mut sink = io::sink();
    let mut hashing = Hashing::new(&mut sink);
    io::copy(input, &mut hashing)?;
    Ok(hashing.finish())
}

/// Whether a binary of this size and SHA-256 is the pinned build.
pub fn is_pinned_binary(pin: &NodePin, size: u64, sha256: &str) -> bool {
    size == pin.binary_size && sha256 == pin.binary_sha256
}

/// What an archive is read from.
pub trait Archive: Read + Seek {}
impl<T: Read + Seek> Archive for T {}

/// Copy the pinned binary out of `archive` into `out`, and check it is the
/// pinned build. Only that one member is read, and nothing else in the
/// archive is written anywhere.
pub fn extract(
    pin: &NodePin,
    archive: &mut dyn Archive,
    out: &mut dyn Write,
) -> Result<(), String> {
    let mut hashing = Hashing::new(out);
    match pin.packing {
        Packing::TarXz => from_tar(
            &mut lzma_rust2::XzReader::new(archive, false),
            pin,
            &mut hashing,
        ),
        Packing::Zip => from_zip(archive, pin, &mut hashing),
    }?;
    hashing.flush().map_err(unpacking)?;
    let (size, sha256) = hashing.finish();
    if !is_pinned_binary(pin, size, &sha256) {
        return Err("The Node.js binary in the download is not the pinned build.".into());
    }
    Ok(())
}

fn from_tar(input: &mut dyn Read, pin: &NodePin, out: &mut dyn Write) -> Result<(), String> {
    let mut archive = tar::Archive::new(input);
    for entry in archive.entries().map_err(unpacking)? {
        let mut entry = entry.map_err(unpacking)?;
        let regular = entry.header().entry_type() == tar::EntryType::Regular;
        if regular && entry.path_bytes().as_ref() == pin.member.as_bytes() {
            return copy_at_most(&mut entry, pin.binary_size, out);
        }
    }
    Err(not_in_archive(pin))
}

fn from_zip(input: &mut dyn Archive, pin: &NodePin, out: &mut dyn Write) -> Result<(), String> {
    let mut archive = zip::ZipArchive::new(input).map_err(unpacking)?;
    let copied = match archive.by_name(pin.member) {
        Ok(mut file) if file.is_file() => copy_at_most(&mut file, pin.binary_size, out),
        _ => Err(not_in_archive(pin)),
    };
    copied
}

/// Refuses a member longer than the pin says, rather than writing all of it.
fn copy_at_most(member: &mut dyn Read, limit: u64, out: &mut dyn Write) -> Result<(), String> {
    let copied = io::copy(&mut member.take(limit + 1), out).map_err(unpacking)?;
    if copied > limit {
        return Err("The Node.js binary in the download is larger than the pinned build.".into());
    }
    Ok(())
}

fn not_in_archive(pin: &NodePin) -> String {
    format!("The download has no {}.", pin.member)
}

fn unpacking(error: impl std::fmt::Display) -> String {
    format!("Could not unpack Node.js: {error}")
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn pins_a_build_for_each_desktop_target_and_nothing_else() {
        for (os, arch) in [
            ("macos", "aarch64"),
            ("macos", "x86_64"),
            ("linux", "aarch64"),
            ("linux", "x86_64"),
            ("windows", "x86_64"),
            ("windows", "aarch64"),
        ] {
            let pin = target_pin(os, arch).expect(os);
            let stem = pin
                .archive
                .trim_end_matches(".tar.xz")
                .trim_end_matches(".zip");
            assert!(stem.starts_with("node-v24.22.1-"), "{stem}");
            assert!(pin.member.starts_with(&format!("{stem}/")));
            assert_eq!(pin.archive_sha256.len(), 64);
            assert_eq!(pin.binary_sha256.len(), 64);
            // Each 25 to 40 MB to download, and 80 to 130 MB unpacked.
            assert!((25_000_000..40_000_000).contains(&pin.archive_size));
            assert!((80_000_000..130_000_000).contains(&pin.binary_size));
            let zip = pin.packing == Packing::Zip;
            assert_eq!(zip, os == "windows");
            assert_eq!(pin.archive.ends_with(".zip"), zip);
            assert_eq!(pin.archive.ends_with(".tar.xz"), !zip);
            assert_eq!(pin.member.ends_with("/node.exe"), zip);
            assert_eq!(pin.member.ends_with("/bin/node"), !zip);
        }
        assert!(target_pin("freebsd", "x86_64").is_none());
        assert!(target_pin("linux", "riscv64").is_none());
        assert_eq!(
            current_pin(),
            target_pin(std::env::consts::OS, std::env::consts::ARCH)
        );
        assert!(current_pin().is_some());
    }

    #[test]
    fn downloads_from_nodejs_org_over_https() {
        let pin = target_pin("linux", "x86_64").unwrap();
        assert_eq!(
            download_url(pin),
            "https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-x64.tar.xz"
        );
        assert_eq!(
            needs_node(),
            "Extensions need Node.js v24.22.1. Download it from the Extensions page."
        );
        assert!(BINARY_NAME.starts_with("node"));
    }

    #[test]
    fn knows_a_leftover_download() {
        assert!(is_leftover(".node-download-1-2-3.tmp"));
        assert!(is_leftover(".node-bin-1-2-3.tmp"));
        assert!(!is_leftover("node"));
        assert!(!is_leftover("node.exe"));
        assert!(!is_leftover(".node-download-1"));
        assert!(!is_leftover("x.node-1.tmp"));
    }

    #[test]
    fn tells_progress_once_per_whole_percent_and_never_past_100() {
        assert_eq!(percent(0, 0), 0);
        assert_eq!(percent(5, 0), 0);
        assert_eq!(percent(1, 3), 33);
        assert_eq!(percent(3, 3), 100);
        assert_eq!(percent(9, 3), 100);
        let mut steps = Steps::default();
        assert_eq!(steps.advance(0, 1000), Some(0));
        assert_eq!(steps.advance(5, 1000), None);
        assert_eq!(steps.advance(10, 1000), Some(1));
        assert_eq!(steps.advance(19, 1000), None);
        assert_eq!(steps.advance(5000, 1000), Some(100));
        assert_eq!(steps.advance(6000, 1000), None);
    }

    #[test]
    fn hashes_what_passes_through() {
        let mut out = Vec::new();
        let mut hashing = Hashing::new(&mut out);
        hashing.write_all(b"hello ").unwrap();
        hashing.write_all(b"world").unwrap();
        hashing.flush().unwrap();
        let hello = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9";
        assert_eq!(hashing.finish(), (11, hello.to_string()));
        assert_eq!(out, b"hello world");
        assert_eq!(
            sha256_of(&mut Cursor::new(b"hello world")).unwrap(),
            (11, hello.to_string())
        );
        assert!(sha256_of(&mut Broken).is_err());
        assert!(Hashing::new(&mut Broken).flush().is_err());
        assert!(Hashing::new(&mut Broken).write(b"x").is_err());
        assert_eq!(
            Hasher::default().finish(),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
    }

    /// Fails every read, write and flush.
    struct Broken;

    impl Read for Broken {
        fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
            Err(io::Error::other("broken"))
        }
    }

    impl Write for Broken {
        fn write(&mut self, _: &[u8]) -> io::Result<usize> {
            Err(io::Error::other("broken"))
        }
        fn flush(&mut self) -> io::Result<()> {
            Err(io::Error::other("broken"))
        }
    }

    /// Takes everything, then fails to flush.
    struct Unflushable;

    impl Write for Unflushable {
        fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
            Ok(buf.len())
        }
        fn flush(&mut self) -> io::Result<()> {
            Err(io::Error::other("full"))
        }
    }

    /// A pin for `binary`, packed as `packing` at `member`.
    fn pin_for(packing: Packing, member: &'static str, binary: &[u8]) -> NodePin {
        let mut hasher = Hasher::default();
        hasher.update(binary);
        NodePin {
            archive: "test",
            archive_sha256: "",
            archive_size: 0,
            packing,
            member,
            binary_sha256: Box::leak(hasher.finish().into_boxed_str()),
            binary_size: binary.len() as u64,
        }
    }

    fn xz(bytes: &[u8]) -> Vec<u8> {
        let mut xz =
            lzma_rust2::XzWriter::new(Vec::new(), lzma_rust2::XzOptions::default()).unwrap();
        xz.write_all(bytes).unwrap();
        xz.finish().unwrap()
    }

    /// A tar.xz holding `files`: each a path, a type and its bytes.
    fn tar_xz(files: &[(&str, tar::EntryType, &[u8])]) -> Vec<u8> {
        let mut builder = tar::Builder::new(Vec::new());
        for (path, kind, bytes) in files {
            let mut header = tar::Header::new_gnu();
            header.set_entry_type(*kind);
            header.set_size(bytes.len() as u64);
            header.set_mode(0o755);
            builder.append_data(&mut header, path, *bytes).unwrap();
        }
        xz(&builder.into_inner().unwrap())
    }

    /// A zip holding `files`, deflated as nodejs.org's are; a path ending in
    /// `/` is a folder.
    fn zip_of(files: &[(&str, &[u8])]) -> Vec<u8> {
        let mut writer = zip::ZipWriter::new(Cursor::new(Vec::new()));
        let options = zip::write::SimpleFileOptions::default()
            .compression_method(zip::CompressionMethod::Deflated);
        for (path, bytes) in files {
            if path.ends_with('/') {
                writer.add_directory(*path, options).unwrap();
            } else {
                writer.start_file(*path, options).unwrap();
                writer.write_all(bytes).unwrap();
            }
        }
        writer.finish().unwrap().into_inner()
    }

    fn extracted(pin: &NodePin, archive: Vec<u8>) -> Result<Vec<u8>, String> {
        let mut out = Vec::new();
        extract(pin, &mut Cursor::new(archive), &mut out).map(|()| out)
    }

    const BINARY: &[u8] = b"\x7fELF the pinned node";

    #[test]
    fn takes_only_the_pinned_binary_out_of_a_tar_xz() {
        let pin = pin_for(Packing::TarXz, "node-v1/bin/node", BINARY);
        let archive = tar_xz(&[
            ("node-v1/LICENSE", tar::EntryType::Regular, b"MIT"),
            ("node-v1/bin/node", tar::EntryType::Symlink, b""),
            ("node-v1/bin/node", tar::EntryType::Regular, BINARY),
            ("node-v1/bin/npm", tar::EntryType::Regular, b"npm"),
        ]);
        assert_eq!(extracted(&pin, archive).unwrap(), BINARY);
    }

    #[test]
    fn takes_only_the_pinned_binary_out_of_a_zip() {
        let pin = pin_for(Packing::Zip, "node-v1/node.exe", BINARY);
        let archive = zip_of(&[
            ("node-v1/", b""),
            ("node-v1/npm", b"npm"),
            ("node-v1/node.exe", BINARY),
        ]);
        assert_eq!(extracted(&pin, archive).unwrap(), BINARY);
    }

    #[test]
    fn refuses_an_archive_without_the_binary() {
        let pin = pin_for(Packing::TarXz, "node-v1/bin/node", BINARY);
        for archive in [
            tar_xz(&[("node-v1/bin/npm", tar::EntryType::Regular, BINARY)]),
            // A link where the binary should be is not the binary.
            tar_xz(&[("node-v1/bin/node", tar::EntryType::Symlink, b"")]),
        ] {
            assert_eq!(
                extracted(&pin, archive),
                Err("The download has no node-v1/bin/node.".into())
            );
        }
        let pin = pin_for(Packing::Zip, "node-v1/node.exe", BINARY);
        assert_eq!(
            extracted(&pin, zip_of(&[("node-v1/npm", BINARY)])),
            Err("The download has no node-v1/node.exe.".into())
        );
        // A folder where the binary should be is not the binary either.
        let pin = NodePin {
            member: "node-v1/node.exe/",
            ..pin
        };
        assert_eq!(
            extracted(&pin, zip_of(&[("node-v1/node.exe/", b"")])),
            Err("The download has no node-v1/node.exe/.".into())
        );
    }

    #[test]
    fn refuses_a_binary_that_is_not_the_pinned_build() {
        let pin = pin_for(Packing::TarXz, "node-v1/bin/node", BINARY);
        let tampered = b"\x7fELF the pinned nodf";
        let shorter = b"\x7fELF";
        for binary in [&tampered[..], &shorter[..]] {
            let archive = tar_xz(&[("node-v1/bin/node", tar::EntryType::Regular, binary)]);
            assert_eq!(
                extracted(&pin, archive),
                Err("The Node.js binary in the download is not the pinned build.".into())
            );
        }
        let longer = [BINARY, b"!"].concat();
        let archive = tar_xz(&[("node-v1/bin/node", tar::EntryType::Regular, &longer)]);
        assert_eq!(
            extracted(&pin, archive),
            Err("The Node.js binary in the download is larger than the pinned build.".into())
        );
    }

    #[test]
    fn says_why_an_archive_could_not_be_unpacked() {
        let tar_pin = pin_for(Packing::TarXz, "node-v1/bin/node", BINARY);
        let zip_pin = pin_for(Packing::Zip, "node-v1/node.exe", BINARY);
        for (pin, archive) in [
            (&tar_pin, b"not xz".to_vec()),
            (&zip_pin, b"not zip".to_vec()),
        ] {
            let error = extracted(pin, archive).unwrap_err();
            assert!(error.starts_with("Could not unpack Node.js: "), "{error}");
        }
        // A tar whose second header is damaged.
        let mut tar = tar::Builder::new(Vec::new());
        let mut header = tar::Header::new_gnu();
        header.set_size(1);
        tar.append_data(&mut header, "a", &b"a"[..]).unwrap();
        let mut tar = tar.into_inner().unwrap();
        // In place of the two empty blocks that end it.
        tar.truncate(tar.len() - 1024);
        tar.extend_from_slice(&[0xff; 512]);
        let error = extracted(&tar_pin, xz(&tar)).unwrap_err();
        assert!(error.starts_with("Could not unpack Node.js: "), "{error}");

        // Nowhere to write it, or to flush it to.
        let archive = tar_xz(&[("node-v1/bin/node", tar::EntryType::Regular, BINARY)]);
        let error = extract(&tar_pin, &mut Cursor::new(archive.clone()), &mut Broken);
        assert_eq!(error, Err("Could not unpack Node.js: broken".into()));
        let error = extract(&tar_pin, &mut Cursor::new(archive), &mut Unflushable);
        assert_eq!(error, Err("Could not unpack Node.js: full".into()));
    }
}
