// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What each platform's webview asks for, in the names Electron's permission
//! handler hears, so one broker (`permissions.rs`) answers all three.
//!
//! A kind with no Electron name keeps a name of its own, which the broker
//! refuses without asking, as it refuses anything it does not know.

use std::collections::HashMap;

use super::permission_store::{PermissionDecision, SitePermissions};
use super::permissions::{origin_of, PermissionDetails};

/// One request, as Electron's `session.setPermissionRequestHandler` sees it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Ask {
    pub permission: String,
    pub details: PermissionDetails,
}

/// What to do with a platform's request.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NativeAsk {
    /// The broker's to answer, asking the person if it has to.
    Request(Ask),
    /// Never asked about: granted only when the site already has one of
    /// these. WebKit's device labels are the camera's or the microphone's.
    AnyAllowed(Vec<Ask>),
    /// Granted without asking, as Electron's guest has it without a request.
    Granted,
    /// Refused without asking, as Electron's guest never gets it.
    Refused,
    /// The camera or the microphone alone, which WebView2 raises as two
    /// requests for one `getUserMedia({audio, video})`: the halves that come
    /// together are one question, as Electron asks it.
    MediaPart(Ask),
}

fn ask(permission: &str, url: &str) -> Ask {
    Ask {
        permission: permission.to_string(),
        details: PermissionDetails {
            requesting_url: Some(url.to_string()),
            ..PermissionDetails::default()
        },
    }
}

fn media(types: &[&str], url: &str) -> Ask {
    Ask {
        permission: "media".to_string(),
        details: PermissionDetails {
            media_types: Some(types.iter().map(|t| t.to_string()).collect()),
            requesting_url: Some(url.to_string()),
            media_type: None,
        },
    }
}

/// A WebKitGTK `permission-request`, by its request's GObject type name;
/// `audio`, `video` and `display` are a user media request's devices. `url`
/// is the guest's committed address ([`CommittedPages`]): WebKitGTK names no
/// frame, so a frame's request is the page's, which Permissions Policy keeps
/// to the frames the page delegated to.
///
/// A screen is refused: Electron's guest has no `getDisplayMedia`. So is
/// storage access, which is always a third-party frame's, and which WebKit
/// names only by domain, so it could never be kept per frame as Electron
/// keeps it.
pub fn webkit_ask(
    type_name: &str,
    audio: bool,
    video: bool,
    display: bool,
    url: &str,
) -> NativeAsk {
    let name = match type_name {
        "WebKitUserMediaPermissionRequest" if display => return NativeAsk::Refused,
        "WebKitUserMediaPermissionRequest" => {
            let types: Vec<&str> = [(video, "video"), (audio, "audio")]
                .into_iter()
                .filter_map(|(wanted, kind)| wanted.then_some(kind))
                .collect();
            return NativeAsk::Request(media(&types, url));
        }
        "WebKitDeviceInfoPermissionRequest" => {
            return NativeAsk::AnyAllowed(vec![media(&["video"], url), media(&["audio"], url)]);
        }
        "WebKitGeolocationPermissionRequest" => "geolocation",
        "WebKitNotificationPermissionRequest" => "notifications",
        "WebKitPointerLockPermissionRequest" => "pointerLock",
        "WebKitClipboardPermissionRequest" => "clipboard-read",
        "WebKitWebsiteDataAccessPermissionRequest" => return NativeAsk::Refused,
        // Encrypted media and missing codecs, which Electron never asks about.
        other => other,
    };
    NativeAsk::Request(ask(name, url))
}

/// A WebView2 `PermissionRequested`, by its `COREWEBVIEW2_PERMISSION_KIND`;
/// `url` is the frame that asked.
pub fn webview2_ask(kind: i32, url: &str) -> NativeAsk {
    let name = match kind {
        1 => return NativeAsk::MediaPart(media(&["audio"], url)),
        2 => return NativeAsk::MediaPart(media(&["video"], url)),
        3 => "geolocation",
        4 => "notifications",
        5 => "sensors",
        6 => "clipboard-read",
        7 => "automatic-downloads",
        8 => "fileSystem",
        // Chromium in Electron plays sound without asking.
        9 => return NativeAsk::Granted,
        10 => "local-fonts",
        11 => "midiSysex",
        12 => "window-management",
        _ => "unknown",
    };
    NativeAsk::Request(ask(name, url))
}

/// Whether the app's own window refuses a WebView2 `PermissionRequested` of
/// `kind` without asking: notifications. The window never notifies through
/// its webview, and WebView2 would otherwise prompt for them itself (the
/// Workspace settings' Enable Push).
pub fn main_refuses_webview2(kind: i32) -> bool {
    kind == 4
}

/// A WKWebView media capture request, by its `WKMediaCaptureType`; `url` is
/// the asking frame's origin ([`security_origin_url`]).
pub fn wk_media_ask(capture_type: isize, url: &str) -> NativeAsk {
    let types: &[&str] = match capture_type {
        0 => &["video"],
        1 => &["audio"],
        2 => &["video", "audio"],
        _ => &[],
    };
    NativeAsk::Request(media(types, url))
}

/// A `WKSecurityOrigin` as an address: its port is 0 when it is the
/// scheme's own.
pub fn security_origin_url(protocol: &str, host: &str, port: isize) -> String {
    if port == 0 {
        format!("{protocol}://{host}/")
    } else {
        format!("{protocol}://{host}:{port}/")
    }
}

/// A step of a WebKitGTK main-frame load, as `load-changed` reports it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WebKitLoad {
    Started,
    Redirected,
    Committed,
    Finished,
}

/// Each WebKitGTK guest's committed address, which its permission requests
/// come from. `webkit_web_view_get_uri` is already the next page's once a
/// load starts, while the old page still runs and can still ask: it would
/// ask with the next site's answers.
#[derive(Debug, Default)]
pub struct CommittedPages(HashMap<String, String>);

impl CommittedPages {
    pub fn load_changed(&mut self, label: &str, step: WebKitLoad, uri: &str) {
        if step == WebKitLoad::Committed {
            self.0.insert(label.to_string(), uri.to_string());
        }
    }

    /// Empty before anything has committed, which the broker refuses.
    pub fn requesting_url(&self, label: &str) -> String {
        self.0.get(label).cloned().unwrap_or_default()
    }

    pub fn forget(&mut self, label: &str) {
        self.0.remove(label);
    }
}

/// The sites a WebKitGTK guest's new web process is told may, or may never,
/// show notifications, before any page asks
/// (`webkit_web_context_initialize_notification_permissions`): an allowed
/// site reads "granted" without asking. WebKitGTK 2.52 still reads a blocked
/// one as "default", and when it asks the stored Block answers with no bar.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct NotificationSeeds {
    pub allowed: Vec<String>,
    pub blocked: Vec<String>,
}

impl NotificationSeeds {
    pub fn from_sites(sites: &[SitePermissions]) -> Self {
        let mut seeds = Self::default();
        for site in sites {
            let entries = site.permissions.iter();
            for entry in entries.filter(|e| e.permission == "notifications") {
                let list = match entry.decision {
                    PermissionDecision::Allow => &mut seeds.allowed,
                    PermissionDecision::Block => &mut seeds.blocked,
                };
                list.push(site.origin.clone());
            }
        }
        seeds
    }

    /// Whether the page at `url` may show a notification now. Asked at each
    /// one: WebKit keeps telling a page "granted" after its site's Allow is
    /// removed, until its web process is replaced.
    pub fn allows(&self, url: &str) -> bool {
        let origin = origin_of(url);
        self.allowed.contains(&origin)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::side_panel::permissions::permission_keys;

    const URL: &str = "https://meet.example/room";

    fn request(native: NativeAsk) -> Option<Ask> {
        match native {
            NativeAsk::Request(ask) | NativeAsk::MediaPart(ask) => Some(ask),
            _ => None,
        }
    }

    /// The keys the broker would remember a request under; empty when it
    /// refuses without asking.
    fn keys(native: NativeAsk) -> Vec<String> {
        let ask = request(native).expect("a request");
        assert_eq!(ask.details.requesting_url.as_deref(), Some(URL));
        permission_keys(&ask.permission, Some(&ask.details))
    }

    #[test]
    fn webkit_user_media_splits_into_the_camera_and_the_microphone() {
        let media = |audio, video| {
            keys(webkit_ask(
                "WebKitUserMediaPermissionRequest",
                audio,
                video,
                false,
                URL,
            ))
        };
        assert_eq!(media(false, true), ["camera"]);
        assert_eq!(media(true, false), ["microphone"]);
        assert_eq!(media(true, true), ["camera", "microphone"]);
        assert!(media(false, false).is_empty());
    }

    #[test]
    fn webkit_refuses_a_screen_and_storage_access_without_asking() {
        assert_eq!(
            webkit_ask("WebKitUserMediaPermissionRequest", true, true, true, URL),
            NativeAsk::Refused
        );
        assert_eq!(
            webkit_ask(
                "WebKitWebsiteDataAccessPermissionRequest",
                false,
                false,
                false,
                URL
            ),
            NativeAsk::Refused
        );
    }

    #[test]
    fn webkit_kinds_are_electrons_names() {
        for (type_name, key) in [
            ("WebKitGeolocationPermissionRequest", "geolocation"),
            ("WebKitNotificationPermissionRequest", "notifications"),
            ("WebKitPointerLockPermissionRequest", "pointer-lock"),
            ("WebKitClipboardPermissionRequest", "clipboard-read"),
        ] {
            assert_eq!(keys(webkit_ask(type_name, false, false, false, URL)), [key]);
        }
        for type_name in [
            "WebKitMediaKeySystemPermissionRequest",
            "WebKitInstallMissingMediaPluginsPermissionRequest",
            "",
        ] {
            assert!(
                keys(webkit_ask(type_name, false, false, false, URL)).is_empty(),
                "{type_name}"
            );
        }
    }

    #[test]
    fn webkit_device_labels_follow_the_camera_or_the_microphone() {
        let native = webkit_ask(
            "WebKitDeviceInfoPermissionRequest",
            false,
            false,
            false,
            URL,
        );
        assert_eq!(request(native.clone()), None);
        assert_eq!(
            native,
            NativeAsk::AnyAllowed(vec![media(&["video"], URL), media(&["audio"], URL)])
        );
        let camera = media(&["video"], URL);
        assert_eq!(
            permission_keys(&camera.permission, Some(&camera.details)),
            ["camera"]
        );
    }

    #[test]
    fn webview2_kinds_are_electrons_names() {
        for (kind, key) in [
            (1, "microphone"),
            (2, "camera"),
            (3, "geolocation"),
            (4, "notifications"),
            (6, "clipboard-read"),
            (10, "local-fonts"),
            (11, "midi"),
            (12, "window-management"),
        ] {
            assert_eq!(keys(webview2_ask(kind, URL)), [key], "{kind}");
        }
        for kind in [0, 5, 7, 8, 13, -1] {
            assert!(keys(webview2_ask(kind, URL)).is_empty(), "{kind}");
        }
        assert_eq!(webview2_ask(9, URL), NativeAsk::Granted);
        assert_eq!(
            webview2_ask(2, URL),
            NativeAsk::MediaPart(media(&["video"], URL))
        );
    }

    #[test]
    fn the_apps_window_refuses_notifications_alone() {
        let refused: Vec<i32> = (-1..=13).filter(|&k| main_refuses_webview2(k)).collect();
        assert_eq!(refused, [4]);
        assert_eq!(keys(webview2_ask(4, URL)), ["notifications"]);
    }

    #[test]
    fn a_webkit_request_comes_from_the_committed_page_not_the_one_loading() {
        let mut pages = CommittedPages::default();
        assert_eq!(pages.requesting_url("panel-guest-1"), "");
        pages.load_changed("panel-guest-1", WebKitLoad::Started, "https://a.example/");
        assert_eq!(
            pages.requesting_url("panel-guest-1"),
            "",
            "nothing committed"
        );
        pages.load_changed("panel-guest-1", WebKitLoad::Committed, "https://a.example/");
        pages.load_changed("panel-guest-1", WebKitLoad::Finished, "https://a.example/");
        // `location = B`: A still runs until B commits.
        pages.load_changed("panel-guest-1", WebKitLoad::Started, "https://b.example/");
        pages.load_changed(
            "panel-guest-1",
            WebKitLoad::Redirected,
            "https://b.example/x",
        );
        assert_eq!(pages.requesting_url("panel-guest-1"), "https://a.example/");
        assert_eq!(pages.requesting_url("panel-guest-2"), "", "another guest's");
        pages.load_changed(
            "panel-guest-1",
            WebKitLoad::Committed,
            "https://b.example/x",
        );
        assert_eq!(pages.requesting_url("panel-guest-1"), "https://b.example/x");
        pages.forget("panel-guest-1");
        assert_eq!(pages.requesting_url("panel-guest-1"), "");
    }

    #[test]
    fn wkwebview_capture_types_are_the_camera_and_the_microphone() {
        assert_eq!(keys(wk_media_ask(0, URL)), ["camera"]);
        assert_eq!(keys(wk_media_ask(1, URL)), ["microphone"]);
        assert_eq!(keys(wk_media_ask(2, URL)), ["camera", "microphone"]);
        assert!(keys(wk_media_ask(3, URL)).is_empty());
    }

    #[test]
    fn a_security_origin_is_an_address_with_its_port_only_when_it_has_one() {
        assert_eq!(
            security_origin_url("https", "meet.example", 0),
            "https://meet.example/"
        );
        assert_eq!(
            security_origin_url("http", "localhost", 8080),
            "http://localhost:8080/"
        );
    }

    fn site(origin: &str, entries: &[(&str, PermissionDecision)]) -> SitePermissions {
        use crate::side_panel::permission_store::PermissionEntry;
        let permissions = entries
            .iter()
            .map(|(permission, decision)| PermissionEntry {
                permission: permission.to_string(),
                decision: *decision,
            });
        SitePermissions {
            origin: origin.to_string(),
            permissions: permissions.collect(),
        }
    }

    #[test]
    fn notification_seeds_are_the_sites_answers_to_notifications_alone() {
        use PermissionDecision::{Allow, Block};
        let sites = [
            site(
                "https://chat.example",
                &[("camera", Block), ("notifications", Allow)],
            ),
            site("https://ads.example", &[("notifications", Block)]),
            site("https://maps.example", &[("geolocation", Allow)]),
            site("http://127.0.0.1:8080", &[("notifications", Allow)]),
        ];
        let seeds = NotificationSeeds::from_sites(&sites);
        assert_eq!(
            seeds,
            NotificationSeeds {
                allowed: vec![
                    "https://chat.example".into(),
                    "http://127.0.0.1:8080".into()
                ],
                blocked: vec!["https://ads.example".into()],
            }
        );
        assert_eq!(
            NotificationSeeds::from_sites(&[]),
            NotificationSeeds::default()
        );
    }

    #[test]
    fn a_page_may_notify_only_while_its_site_is_allowed() {
        let seeds = NotificationSeeds {
            allowed: vec![
                "https://chat.example".into(),
                "http://127.0.0.1:8080".into(),
            ],
            blocked: vec!["https://ads.example".into()],
        };
        assert!(seeds.allows("https://chat.example/room/1?x#y"));
        assert!(seeds.allows("http://127.0.0.1:8080/notify"));
        assert!(!seeds.allows("http://127.0.0.1:8081/notify"));
        assert!(!seeds.allows("https://ads.example/"));
        assert!(!seeds.allows("https://maps.example/"));
        assert!(!seeds.allows(""));
        assert!(!NotificationSeeds::default().allows("https://chat.example/"));
    }
}
