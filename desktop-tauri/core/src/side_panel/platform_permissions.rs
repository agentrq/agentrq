// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What each platform's webview asks for, in the names Electron's permission
//! handler hears, so one broker (`permissions.rs`) answers all three.
//!
//! A kind with no Electron name keeps a name of its own, which the broker
//! refuses without asking, as it refuses anything it does not know.

use super::permissions::PermissionDetails;

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
/// is the guest's address.
pub fn webkit_ask(
    type_name: &str,
    audio: bool,
    video: bool,
    display: bool,
    url: &str,
) -> NativeAsk {
    let name = match type_name {
        "WebKitUserMediaPermissionRequest" if display => "display-capture",
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
        "WebKitWebsiteDataAccessPermissionRequest" => "storage-access",
        // Encrypted media and missing codecs, which Electron never asks about.
        other => other,
    };
    NativeAsk::Request(ask(name, url))
}

/// A WebView2 `PermissionRequested`, by its `COREWEBVIEW2_PERMISSION_KIND`;
/// `url` is the frame that asked.
pub fn webview2_ask(kind: i32, url: &str) -> NativeAsk {
    let name = match kind {
        1 => return NativeAsk::Request(media(&["audio"], url)),
        2 => return NativeAsk::Request(media(&["video"], url)),
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::side_panel::permissions::permission_keys;

    const URL: &str = "https://meet.example/room";

    fn request(native: NativeAsk) -> Option<Ask> {
        match native {
            NativeAsk::Request(ask) => Some(ask),
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
        assert_eq!(
            keys(webkit_ask(
                "WebKitUserMediaPermissionRequest",
                true,
                true,
                true,
                URL
            )),
            ["display-capture"]
        );
    }

    #[test]
    fn webkit_kinds_are_electrons_names() {
        for (type_name, key) in [
            ("WebKitGeolocationPermissionRequest", "geolocation"),
            ("WebKitNotificationPermissionRequest", "notifications"),
            ("WebKitPointerLockPermissionRequest", "pointer-lock"),
            ("WebKitClipboardPermissionRequest", "clipboard-read"),
            ("WebKitWebsiteDataAccessPermissionRequest", "storage-access"),
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
}
