// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What somebody answered when a page in the side panel asked for a permission.
//!
//! Kept on this machine only — a JSON file per profile in the app's own data
//! folder — and never sent anywhere: which sites somebody lets use their camera
//! is theirs. One file per profile, so forgetting a profile can delete it.
//!
//! Every failure is survivable. A file that cannot be read is an empty record,
//! so every page asks again rather than the panel refusing to start; a write
//! that fails keeps the answer for this run and says so in the log.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

pub const PERMISSIONS_DIR: &str = "side-panel-permissions";

/// The contract a permission store satisfies for the broker.
pub trait PermissionStoreBackend: Send + Sync {
    /// Read the permissions file into memory.
    ///
    /// # Blocking I/O
    /// Performs synchronous file reads. The Tauri glue must call this off the
    /// UI / main thread.
    fn load(&self);
    fn get(&self, origin: &str, permission: &str) -> Option<PermissionDecision>;
    fn set(&self, origin: &str, permission: &str, decision: PermissionDecision);
}

/// Only the two answers a person can give.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum PermissionDecision {
    Allow,
    Block,
}

impl PermissionDecision {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Allow => "allow",
            Self::Block => "block",
        }
    }

    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "allow" => Some(Self::Allow),
            "block" => Some(Self::Block),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PermissionEntry {
    pub permission: String,
    pub decision: PermissionDecision,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SitePermissions {
    pub origin: String,
    pub permissions: Vec<PermissionEntry>,
}

/// Percent-encode a profile ID for a safe filename, matching JavaScript's `encodeURIComponent`.
pub fn encode_uri_component(input: &str) -> String {
    let mut out = String::with_capacity(input.len());
    for b in input.bytes() {
        if b.is_ascii_alphanumeric()
            || matches!(
                b,
                b'-' | b'_' | b'.' | b'!' | b'~' | b'*' | b'\'' | b'(' | b')'
            )
        {
            out.push(b as char);
        } else {
            use std::fmt::Write;
            let _ = write!(out, "%{:02X}", b);
        }
    }
    out
}

#[derive(Debug, Clone)]
pub struct FsError {
    pub is_not_found: bool,
    pub message: String,
}

impl FsError {
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            is_not_found: false,
            message: message.into(),
        }
    }

    pub fn not_found(message: impl Into<String>) -> Self {
        Self {
            is_not_found: true,
            message: message.into(),
        }
    }
}

impl From<std::io::Error> for FsError {
    fn from(err: std::io::Error) -> Self {
        Self {
            is_not_found: err.kind() == std::io::ErrorKind::NotFound,
            message: err.to_string(),
        }
    }
}

pub trait PermissionFs: Send + Sync {
    fn read_file(&self, path: &Path) -> Result<String, FsError>;
    fn write_file(&self, path: &Path, contents: &str) -> Result<(), FsError>;
    fn mkdir(&self, path: &Path) -> Result<(), FsError>;
    fn rename(&self, from: &Path, to: &Path) -> Result<(), FsError>;
    fn rm(&self, path: &Path) -> Result<(), FsError>;
}

#[derive(Debug, Default, Clone, Copy)]
pub struct StdFs;

impl PermissionFs for StdFs {
    fn read_file(&self, path: &Path) -> Result<String, FsError> {
        std::fs::read_to_string(path).map_err(FsError::from)
    }

    fn write_file(&self, path: &Path, contents: &str) -> Result<(), FsError> {
        std::fs::write(path, contents).map_err(FsError::from)
    }

    fn mkdir(&self, path: &Path) -> Result<(), FsError> {
        std::fs::create_dir_all(path).map_err(FsError::from)
    }

    fn rename(&self, from: &Path, to: &Path) -> Result<(), FsError> {
        std::fs::rename(from, to).map_err(FsError::from)
    }

    fn rm(&self, path: &Path) -> Result<(), FsError> {
        match std::fs::remove_file(path) {
            Ok(()) => Ok(()),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(err) => Err(FsError::from(err)),
        }
    }
}

pub trait PermissionLogger: Send + Sync {
    fn warn(&self, message: &str);
}

#[derive(Clone)]
pub struct ConsoleLogger {
    sink: Arc<dyn Fn(&str) + Send + Sync>,
}

impl Default for ConsoleLogger {
    fn default() -> Self {
        Self {
            sink: Arc::new(|msg| eprintln!("{msg}")),
        }
    }
}

impl ConsoleLogger {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with_sink(sink: impl Fn(&str) + Send + Sync + 'static) -> Self {
        Self {
            sink: Arc::new(sink),
        }
    }
}

impl PermissionLogger for ConsoleLogger {
    fn warn(&self, message: &str) {
        (self.sink)(message);
    }
}

#[derive(Default)]
struct StoreState {
    loaded: bool,
    record: BTreeMap<String, BTreeMap<String, PermissionDecision>>,
}

pub struct PermissionStore<F: PermissionFs = StdFs, L: PermissionLogger = ConsoleLogger> {
    folder: PathBuf,
    file: PathBuf,
    fs: F,
    logger: L,
    state: Mutex<StoreState>,
    persist_lock: Mutex<()>,
}

impl PermissionStore<StdFs, ConsoleLogger> {
    pub fn new(dir: impl AsRef<Path>, profile_id: &str) -> Self {
        Self::with_fs_and_logger(dir, profile_id, StdFs, ConsoleLogger::default())
    }
}

impl<F: PermissionFs, L: PermissionLogger> PermissionStore<F, L> {
    pub fn with_fs_and_logger(dir: impl AsRef<Path>, profile_id: &str, fs: F, logger: L) -> Self {
        let folder = dir.as_ref().join(PERMISSIONS_DIR);
        let file = folder.join(format!("{}.json", encode_uri_component(profile_id)));
        Self {
            folder,
            file,
            fs,
            logger,
            state: Mutex::new(StoreState::default()),
            persist_lock: Mutex::new(()),
        }
    }

    pub fn folder(&self) -> &Path {
        &self.folder
    }

    pub fn file(&self) -> &Path {
        &self.file
    }

    /// Read the file once; later calls wait on the same read.
    ///
    /// # Blocking I/O
    /// Performs synchronous filesystem reads. The Tauri glue must call this off
    /// the UI / main thread.
    pub fn load(&self) {
        let mut state = self.state.lock().unwrap();
        if state.loaded {
            return;
        }
        state.loaded = true;
        match self.fs.read_file(&self.file) {
            Ok(text) => {
                state.record = parse_permissions(&text, &self.logger);
            }
            Err(err) => {
                if !err.is_not_found {
                    self.logger.warn(&format!(
                        "[side-panel] site permissions could not be read, so every site will ask again: {}",
                        err.message
                    ));
                }
            }
        }
    }

    pub fn get(&self, origin: &str, permission: &str) -> Option<PermissionDecision> {
        let state = self.state.lock().unwrap();
        state.record.get(origin)?.get(permission).copied()
    }

    pub fn set(&self, origin: &str, permission: &str, decision: &str) {
        let Some(decision) = PermissionDecision::parse(decision) else {
            return;
        };
        self.set_decision(origin, permission, decision);
    }

    pub fn set_decision(&self, origin: &str, permission: &str, decision: PermissionDecision) {
        {
            let mut state = self.state.lock().unwrap();
            state
                .record
                .entry(origin.to_string())
                .or_default()
                .insert(permission.to_string(), decision);
        }
        self.persist();
    }

    /// One permission of a site, or every one of them when none is named.
    pub fn remove(&self, origin: &str, permission: Option<&str>) {
        let permission = permission.filter(|p| !p.is_empty());
        {
            let mut state = self.state.lock().unwrap();
            if !state.record.contains_key(origin) {
                return;
            }
            if let Some(permission) = permission {
                let perms = state.record.get_mut(origin).unwrap();
                perms.remove(permission);
                if perms.is_empty() {
                    state.record.remove(origin);
                }
            } else {
                state.record.remove(origin);
            }
        }
        self.persist();
    }

    /// Every decision, by site, for the Site permissions list. Sorted, so it does not shuffle.
    pub fn list(&self) -> Vec<SitePermissions> {
        let state = self.state.lock().unwrap();
        state
            .record
            .iter()
            .map(|(origin, perms)| SitePermissions {
                origin: origin.clone(),
                permissions: perms
                    .iter()
                    .map(|(permission, decision)| PermissionEntry {
                        permission: permission.clone(),
                        decision: *decision,
                    })
                    .collect(),
            })
            .collect()
    }

    /// The profile is gone: so is everything it answered.
    pub fn forget(&self) {
        let mut state = self.state.lock().unwrap();
        state.record.clear();
        drop(state);
        if let Err(err) = self.fs.rm(&self.file) {
            self.logger.warn(&format!(
                "[side-panel] site permissions of a removed profile could not be deleted: {}",
                err.message
            ));
        }
    }

    fn persist(&self) {
        let _persist_guard = self.persist_lock.lock().unwrap();
        let record = {
            let state = self.state.lock().unwrap();
            state.record.clone()
        };

        let temporary = PathBuf::from(format!("{}.tmp", self.file.display()));

        let mut origins = serde_json::Map::new();
        for (origin, permissions) in &record {
            let mut perms_map = serde_json::Map::new();
            for (permission, decision) in permissions {
                perms_map.insert(
                    permission.clone(),
                    Value::String(decision.as_str().to_string()),
                );
            }
            origins.insert(origin.clone(), Value::Object(perms_map));
        }
        let body = serde_json::json!({ "origins": origins });
        let text = serde_json::to_string_pretty(&body).unwrap();

        if let Err(err) = self.fs.mkdir(&self.folder) {
            self.logger.warn(&format!(
                "[side-panel] site permissions could not be saved, and last only until the app quits: {}",
                err.message
            ));
            return;
        }
        if let Err(err) = self.fs.write_file(&temporary, &text) {
            self.logger.warn(&format!(
                "[side-panel] site permissions could not be saved, and last only until the app quits: {}",
                err.message
            ));
            return;
        }
        if let Err(err) = self.fs.rename(&temporary, &self.file) {
            self.logger.warn(&format!(
                "[side-panel] site permissions could not be saved, and last only until the app quits: {}",
                err.message
            ));
        }
    }
}

fn parse_permissions(
    text: &str,
    logger: &dyn PermissionLogger,
) -> BTreeMap<String, BTreeMap<String, PermissionDecision>> {
    let raw: Value = match serde_json::from_str(text) {
        Ok(v) => v,
        Err(err) => {
            logger.warn(&format!(
                "[side-panel] site permissions could not be read, so every site will ask again: {err}"
            ));
            return BTreeMap::new();
        }
    };
    let mut parsed = BTreeMap::new();
    if let Some(origins) = raw.get("origins").and_then(Value::as_object) {
        for (origin, permissions_val) in origins {
            let mut kept = BTreeMap::new();
            if let Some(permissions_obj) = permissions_val.as_object() {
                for (permission, decision_val) in permissions_obj {
                    if let Some(decision_str) = decision_val.as_str() {
                        if let Some(decision) = PermissionDecision::parse(decision_str) {
                            kept.insert(permission.clone(), decision);
                        }
                    }
                }
            }
            if !kept.is_empty() {
                parsed.insert(origin.clone(), kept);
            }
        }
    }
    parsed
}

impl<F: PermissionFs, L: PermissionLogger> PermissionStoreBackend for PermissionStore<F, L> {
    fn load(&self) {
        PermissionStore::load(self);
    }

    fn get(&self, origin: &str, permission: &str) -> Option<PermissionDecision> {
        PermissionStore::get(self, origin, permission)
    }

    fn set(&self, origin: &str, permission: &str, decision: PermissionDecision) {
        PermissionStore::set_decision(self, origin, permission, decision);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::sync::Arc;

    #[derive(Default, Clone)]
    struct MemoryFs {
        files: Arc<Mutex<HashMap<PathBuf, String>>>,
        read_count: Arc<Mutex<usize>>,
        read_error: Arc<Mutex<Option<FsError>>>,
        mkdir_error: Arc<Mutex<Option<FsError>>>,
        write_error: Arc<Mutex<Option<FsError>>>,
        rename_error: Arc<Mutex<Option<FsError>>>,
        rm_error: Arc<Mutex<Option<FsError>>>,
    }

    impl PermissionFs for MemoryFs {
        fn read_file(&self, path: &Path) -> Result<String, FsError> {
            *self.read_count.lock().unwrap() += 1;
            if let Some(err) = self.read_error.lock().unwrap().clone() {
                return Err(err);
            }
            if let Some(content) = self.files.lock().unwrap().get(path) {
                return Ok(content.clone());
            }
            Err(FsError::not_found("ENOENT: no such file or directory"))
        }

        fn write_file(&self, path: &Path, contents: &str) -> Result<(), FsError> {
            if let Some(err) = self.write_error.lock().unwrap().clone() {
                return Err(err);
            }
            self.files
                .lock()
                .unwrap()
                .insert(path.to_path_buf(), contents.to_string());
            Ok(())
        }

        fn mkdir(&self, _path: &Path) -> Result<(), FsError> {
            if let Some(err) = self.mkdir_error.lock().unwrap().clone() {
                return Err(err);
            }
            Ok(())
        }

        fn rename(&self, from: &Path, to: &Path) -> Result<(), FsError> {
            if let Some(err) = self.rename_error.lock().unwrap().clone() {
                return Err(err);
            }
            let mut files = self.files.lock().unwrap();
            let content = files.remove(from).unwrap_or_default();
            files.insert(to.to_path_buf(), content);
            Ok(())
        }

        fn rm(&self, path: &Path) -> Result<(), FsError> {
            if let Some(err) = self.rm_error.lock().unwrap().clone() {
                return Err(err);
            }
            self.files.lock().unwrap().remove(path);
            Ok(())
        }
    }

    #[derive(Default, Clone)]
    struct TestLogger {
        warnings: Arc<Mutex<Vec<String>>>,
    }

    impl PermissionLogger for TestLogger {
        fn warn(&self, message: &str) {
            self.warnings.lock().unwrap().push(message.to_string());
        }
    }

    struct Fixture {
        dir: PathBuf,
        fs: MemoryFs,
        logger: TestLogger,
    }

    impl Fixture {
        fn new() -> Self {
            Self {
                dir: PathBuf::from("/user-data"),
                fs: MemoryFs::default(),
                logger: TestLogger::default(),
            }
        }

        fn store(&self, profile_id: &str) -> PermissionStore<MemoryFs, TestLogger> {
            PermissionStore::with_fs_and_logger(
                &self.dir,
                profile_id,
                self.fs.clone(),
                self.logger.clone(),
            )
        }

        fn file_of(&self, profile_id: &str) -> PathBuf {
            self.dir
                .join(PERMISSIONS_DIR)
                .join(format!("{profile_id}.json"))
        }

        fn warnings(&self) -> Vec<String> {
            self.logger.warnings.lock().unwrap().clone()
        }
    }

    #[test]
    fn starts_empty_and_says_nothing_when_there_is_no_file_yet() {
        let f = Fixture::new();
        let permissions = f.store("p1");
        permissions.load();
        assert_eq!(permissions.list(), vec![]);
        assert_eq!(permissions.get("https://meet.example", "camera"), None);
        assert!(f.warnings().is_empty());
    }

    #[test]
    fn remembers_an_answer_across_launches_on_disk_only() {
        let f = Fixture::new();
        let first = f.store("p1");
        first.load();
        first.set("https://meet.example", "camera", "allow");
        first.set("https://meet.example", "microphone", "block");

        let second = f.store("p1");
        second.load();
        assert_eq!(
            second.get("https://meet.example", "camera"),
            Some(PermissionDecision::Allow)
        );
        assert_eq!(
            second.get("https://meet.example", "microphone"),
            Some(PermissionDecision::Block)
        );

        let file_text = f.fs.read_file(&f.file_of("p1")).unwrap();
        let parsed: Value = serde_json::from_str(&file_text).unwrap();
        assert_eq!(
            parsed,
            serde_json::json!({
                "origins": {
                    "https://meet.example": {
                        "camera": "allow",
                        "microphone": "block"
                    }
                }
            })
        );
    }

    #[test]
    fn reads_the_file_once_however_often_it_is_asked_to() {
        let f = Fixture::new();
        let permissions = f.store("p1");
        permissions.load();
        permissions.load();
        assert_eq!(*f.fs.read_count.lock().unwrap(), 1);
    }

    #[test]
    fn keeps_each_profile_to_its_own_file() {
        let f = Fixture::new();
        let work = f.store("work");
        work.set("https://meet.example", "camera", "allow");

        let home = f.store("home");
        home.load();
        assert_eq!(home.get("https://meet.example", "camera"), None);
    }

    #[test]
    fn names_the_file_safely_whatever_the_profile_id_is() {
        let f = Fixture::new();
        let odd = f.store("../../escape");
        odd.set("https://meet.example", "camera", "allow");

        let escaped_path = f.dir.join(PERMISSIONS_DIR).join("..%2F..%2Fescape.json");
        let content = f.fs.read_file(&escaped_path).unwrap();
        let parsed: Value = serde_json::from_str(&content).unwrap();
        assert!(parsed.get("origins").is_some());
    }

    #[test]
    fn ignores_anything_but_allow_and_block() {
        let f = Fixture::new();
        let permissions = f.store("p1");
        permissions.set("https://meet.example", "camera", "maybe");
        assert_eq!(permissions.list(), vec![]);
    }

    #[test]
    fn keeps_only_valid_answers_from_the_file() {
        let f = Fixture::new();
        let path = f.file_of("p1");
        let content = serde_json::json!({
            "origins": {
                "https://a.example": { "camera": "allow", "microphone": "perhaps", "geolocation": 123 },
                "https://b.example": { "camera": "sometimes" },
                "https://c.example": null
            }
        })
        .to_string();
        f.fs.write_file(&path, &content).unwrap();

        let permissions = f.store("p1");
        permissions.load();
        assert_eq!(
            permissions.list(),
            vec![SitePermissions {
                origin: "https://a.example".to_string(),
                permissions: vec![PermissionEntry {
                    permission: "camera".to_string(),
                    decision: PermissionDecision::Allow,
                }],
            }]
        );
    }

    #[test]
    fn treats_a_file_with_no_origins_as_empty() {
        let f = Fixture::new();
        let path = f.file_of("p1");
        f.fs.write_file(&path, "null").unwrap();

        let permissions = f.store("p1");
        permissions.load();
        assert_eq!(permissions.list(), vec![]);
    }

    #[test]
    fn treats_an_unreadable_file_as_empty_and_says_every_site_will_ask_again() {
        let f = Fixture::new();
        let path = f.file_of("p1");
        f.fs.write_file(&path, "{not json").unwrap();

        let permissions = f.store("p1");
        permissions.load();
        assert_eq!(permissions.list(), vec![]);
        assert!(f.warnings()[0].contains("could not be read, so every site will ask again"));
    }

    #[test]
    fn says_so_when_the_file_exists_but_cannot_be_opened() {
        let f = Fixture::new();
        *f.fs.read_error.lock().unwrap() = Some(FsError::new("EACCES: permission denied"));

        let permissions = f.store("p1");
        permissions.load();
        assert_eq!(permissions.list(), vec![]);
        assert!(f.warnings()[0].contains("EACCES: permission denied"));
    }

    #[test]
    fn keeps_an_answer_for_the_run_when_it_cannot_be_saved_and_says_so() {
        let f = Fixture::new();
        *f.fs.mkdir_error.lock().unwrap() = Some(FsError::new("in the way"));

        let permissions = f.store("p1");
        permissions.set("https://meet.example", "camera", "allow");
        assert_eq!(
            permissions.get("https://meet.example", "camera"),
            Some(PermissionDecision::Allow)
        );
        assert!(f.warnings()[0].contains("could not be saved, and last only until the app quits"));
    }

    #[test]
    fn removes_one_permission_or_a_whole_site() {
        let f = Fixture::new();
        let permissions = f.store("p1");
        permissions.set("https://meet.example", "camera", "allow");
        permissions.set("https://meet.example", "microphone", "allow");
        permissions.set("https://maps.example", "geolocation", "block");

        permissions.remove("https://meet.example", Some("camera"));
        assert_eq!(permissions.get("https://meet.example", "camera"), None);
        assert_eq!(
            permissions.get("https://meet.example", "microphone"),
            Some(PermissionDecision::Allow)
        );

        permissions.remove("https://meet.example", Some("microphone"));
        assert_eq!(
            permissions
                .list()
                .iter()
                .map(|s| s.origin.as_str())
                .collect::<Vec<_>>(),
            vec!["https://maps.example"]
        );

        // Empty string counts as None and removes the whole site
        permissions.remove("https://maps.example", Some(""));
        permissions.remove("https://never.example", None);
        assert_eq!(permissions.list(), vec![]);

        let reread = f.store("p1");
        reread.load();
        assert_eq!(reread.list(), vec![]);
    }

    #[test]
    fn lists_sites_and_permissions_in_a_stable_order() {
        let f = Fixture::new();
        let permissions = f.store("p1");
        permissions.set("https://z.example", "notifications", "block");
        permissions.set("https://a.example", "microphone", "allow");
        permissions.set("https://a.example", "camera", "block");

        assert_eq!(
            permissions.list(),
            vec![
                SitePermissions {
                    origin: "https://a.example".to_string(),
                    permissions: vec![
                        PermissionEntry {
                            permission: "camera".to_string(),
                            decision: PermissionDecision::Block,
                        },
                        PermissionEntry {
                            permission: "microphone".to_string(),
                            decision: PermissionDecision::Allow,
                        },
                    ],
                },
                SitePermissions {
                    origin: "https://z.example".to_string(),
                    permissions: vec![PermissionEntry {
                        permission: "notifications".to_string(),
                        decision: PermissionDecision::Block,
                    },],
                },
            ]
        );
    }

    #[test]
    fn forgets_everything_when_its_profile_is_removed() {
        let f = Fixture::new();
        let permissions = f.store("p1");
        permissions.set("https://meet.example", "camera", "allow");
        permissions.forget();
        assert_eq!(permissions.list(), vec![]);
        assert!(f.fs.read_file(&f.file_of("p1")).is_err());

        // Forgetting twice, or with no file at all, is fine.
        permissions.forget();
    }

    #[test]
    fn says_so_when_a_removed_profiles_file_cannot_be_deleted() {
        let f = Fixture::new();
        *f.fs.rm_error.lock().unwrap() = Some(FsError::new("EBUSY: resource busy"));

        let permissions = f.store("p1");
        permissions.forget();
        assert!(f.warnings()[0].contains("could not be deleted: EBUSY: resource busy"));
    }

    #[test]
    fn logs_to_the_console_when_no_logger_is_given_and_tolerates_a_bare_error() {
        let f = Fixture::new();
        *f.fs.read_error.lock().unwrap() = Some(FsError::new("disk unplugged"));
        *f.fs.mkdir_error.lock().unwrap() = Some(FsError::new("disk unplugged"));
        *f.fs.rm_error.lock().unwrap() = Some(FsError::new("disk unplugged"));

        let captured = Arc::new(Mutex::new(Vec::new()));
        let captured_clone = Arc::clone(&captured);
        let logger = ConsoleLogger::with_sink(move |msg| {
            captured_clone.lock().unwrap().push(msg.to_string());
        });

        let permissions = PermissionStore::with_fs_and_logger(&f.dir, "p1", f.fs.clone(), logger);
        permissions.load();
        permissions.set("https://meet.example", "camera", "allow");
        permissions.forget();

        let warnings = captured.lock().unwrap().clone();
        assert_eq!(warnings.len(), 3);
        for warning in &warnings {
            assert!(warning.contains("disk unplugged"));
        }

        // Exercise default eprintln sink and constructor for 100% line coverage
        let default_logger = ConsoleLogger::default();
        default_logger.warn("test default sink");
        let new_logger = ConsoleLogger::new();
        new_logger.warn("test new logger");
    }

    #[test]
    fn test_stdfs_coverage() {
        let temp_dir = tempfile::tempdir().unwrap();
        let fs = StdFs;
        let sub = temp_dir.path().join("sub");
        fs.mkdir(&sub).unwrap();
        let file = sub.join("test.txt");
        fs.write_file(&file, "hello").unwrap();
        assert_eq!(fs.read_file(&file).unwrap(), "hello");
        let renamed = sub.join("renamed.txt");
        fs.rename(&file, &renamed).unwrap();
        assert_eq!(fs.read_file(&renamed).unwrap(), "hello");
        fs.rm(&renamed).unwrap();
        // rm on non-existent file is ok
        fs.rm(&renamed).unwrap();
        // rm on directory should fail
        assert!(fs.rm(temp_dir.path()).is_err());

        // Constructor with defaults coverage
        let store = PermissionStore::new(temp_dir.path(), "default");
        assert_eq!(store.folder(), temp_dir.path().join(PERMISSIONS_DIR));
        assert_eq!(
            store.file(),
            temp_dir.path().join(PERMISSIONS_DIR).join("default.json")
        );

        // Backend trait methods coverage
        let backend: &dyn PermissionStoreBackend = &store;
        backend.load();
        backend.set("https://example.com", "camera", PermissionDecision::Allow);
        assert_eq!(
            backend.get("https://example.com", "camera"),
            Some(PermissionDecision::Allow)
        );
        store.forget();
    }

    #[test]
    fn test_stdfs_write_rename_read_round_trip() {
        let temp_dir = tempfile::tempdir().unwrap();
        let store = PermissionStore::new(temp_dir.path(), "roundtrip");
        store.set("https://meet.example", "camera", "allow");
        store.set("https://meet.example", "microphone", "block");

        let store2 = PermissionStore::new(temp_dir.path(), "roundtrip");
        store2.load();
        assert_eq!(
            store2.get("https://meet.example", "camera"),
            Some(PermissionDecision::Allow)
        );
        assert_eq!(
            store2.get("https://meet.example", "microphone"),
            Some(PermissionDecision::Block)
        );

        let content = std::fs::read_to_string(store2.file()).unwrap();
        assert!(content.contains("\"camera\": \"allow\""));
        assert!(content.contains("\"microphone\": \"block\""));
    }

    #[test]
    fn concurrent_load_reads_file_once() {
        let f = Fixture::new();
        let store = Arc::new(f.store("p1"));
        let s1 = Arc::clone(&store);
        let s2 = Arc::clone(&store);

        let t1 = std::thread::spawn(move || {
            s1.load();
        });
        let t2 = std::thread::spawn(move || {
            s2.load();
        });

        t1.join().unwrap();
        t2.join().unwrap();

        assert_eq!(*f.fs.read_count.lock().unwrap(), 1);
    }

    #[test]
    fn concurrent_persist_from_multiple_threads() {
        let temp_dir = tempfile::tempdir().unwrap();
        let store = Arc::new(PermissionStore::new(temp_dir.path(), "concurrent"));
        let mut handles = Vec::new();

        for i in 0..10 {
            let s = Arc::clone(&store);
            handles.push(std::thread::spawn(move || {
                s.set(&format!("https://site{i}.example"), "camera", "allow");
            }));
        }

        for handle in handles {
            handle.join().unwrap();
        }

        let reloaded = PermissionStore::new(temp_dir.path(), "concurrent");
        reloaded.load();
        for i in 0..10 {
            assert_eq!(
                reloaded.get(&format!("https://site{i}.example"), "camera"),
                Some(PermissionDecision::Allow)
            );
        }
    }

    #[test]
    fn reads_file_electron_wrote_and_persists_same_shape() {
        let f = Fixture::new();
        // Exact bytes produced by Electron: JSON.stringify({ origins }, null, 2)
        let electron_fixture = "{\n  \"origins\": {\n    \"https://meet.example\": {\n      \"camera\": \"allow\",\n      \"microphone\": \"block\"\n    }\n  }\n}";
        f.fs.write_file(&f.file_of("p1"), electron_fixture).unwrap();

        let store = f.store("p1");
        store.load();
        assert_eq!(
            store.get("https://meet.example", "camera"),
            Some(PermissionDecision::Allow)
        );
        assert_eq!(
            store.get("https://meet.example", "microphone"),
            Some(PermissionDecision::Block)
        );

        // Re-persist the loaded state by setting the same answer
        store.set("https://meet.example", "camera", "allow");

        let persisted = f.fs.read_file(&f.file_of("p1")).unwrap();
        // Parse both as serde_json::Value to assert identical JSON object shape.
        // Note: Key order in the serialized JSON text may differ between Electron and Rust
        // because Rust's BTreeMap sorts object keys alphabetically, whereas JavaScript's
        // Object.fromEntries preserves key insertion order. Electron's JSON.parse does
        // not care about key ordering.
        let parsed_persisted: Value = serde_json::from_str(&persisted).unwrap();
        let parsed_electron: Value = serde_json::from_str(electron_fixture).unwrap();
        assert_eq!(parsed_persisted, parsed_electron);
        assert_eq!(persisted, electron_fixture);
    }

    #[test]
    fn test_fs_error_handling() {
        let f = Fixture::new();
        *f.fs.write_error.lock().unwrap() = Some(FsError::new("write error"));
        let permissions = f.store("p1");
        permissions.set("https://example.com", "camera", "allow");
        assert!(f.warnings()[0].contains("write error"));

        let f2 = Fixture::new();
        *f2.fs.rename_error.lock().unwrap() = Some(FsError::new("rename error"));
        let permissions2 = f2.store("p1");
        permissions2.set("https://example.com", "camera", "allow");
        assert!(f2.warnings()[0].contains("rename error"));
    }
}
