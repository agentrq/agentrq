// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What WebView2 is asked directly for the side panel's guest, because Tauri
//! does not. Native glue, outside the coverage gate: the decisions are
//! `core`'s and `side_panel.rs`'s.

use std::path::PathBuf;

use agentrq_desktop_core::side_panel::child::{Modifiers, Placement};
use tauri::webview::WebviewBuilder;
use tauri::{LogicalPosition, LogicalSize, Runtime, Webview};

pub fn isolate<R: Runtime>(
    builder: WebviewBuilder<R>,
    dir: PathBuf,
    _profile: &str,
) -> WebviewBuilder<R> {
    builder.data_directory(dir)
}

pub fn adopt<R: Runtime>(_main: &Webview<R>, _guest: &Webview<R>, _placement: Placement) {}

pub fn set_bounds<R: Runtime>(guest: &Webview<R>, placement: Placement) {
    let _ = guest.set_bounds(tauri::Rect {
        position: LogicalPosition::new(placement.x, placement.y).into(),
        size: LogicalSize::new(placement.width, placement.height).into(),
    });
}

pub fn forget(_label: &str) {}

/// WebView2 asks for a window on a Ctrl-click, so no navigation is one.
pub fn click(_number: u32) -> Option<Modifiers> {
    None
}

/// A navigation that did not complete, for the panel's own error view; the
/// guest taking focus; and the menu keys its page leaves unhandled.
pub fn watch<R: Runtime>(guest: &Webview<R>) {
    use agentrq_desktop_core::side_panel::child::GuestEvent;
    use tauri::{Emitter, Manager};
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        ICoreWebView2Settings3, COREWEBVIEW2_WEB_ERROR_STATUS,
        COREWEBVIEW2_WEB_ERROR_STATUS_OPERATION_CANCELED,
    };
    use webview2_com::{
        AcceleratorKeyPressedEventHandler, NavigationCompletedEventHandler,
        WebMessageReceivedEventHandler,
    };
    let app = guest.app_handle().clone();
    let label = guest.label().to_string();
    let (focus_app, focus_label) = (app.clone(), label.clone());
    let (keys_app, keys_label) = (app.clone(), label.clone());
    let (said_app, said_label) = (app.clone(), label.clone());
    let _ = guest.with_webview(move |view| unsafe {
        let Ok(core) = view.controller().CoreWebView2() else {
            return;
        };
        // As in `main`: WebView2's own Ctrl+R would reload the guest beside
        // the menu's reload of the app.
        if let Ok(settings) = core
            .Settings()
            .and_then(|s| windows::core::Interface::cast::<ICoreWebView2Settings3>(&s))
        {
            let _ = settings.SetAreBrowserAcceleratorKeysEnabled(false);
        }
        let handler = NavigationCompletedEventHandler::create(Box::new(move |_, args| {
            let Some(args) = args else { return Ok(()) };
            let mut ok = windows::core::BOOL::default();
            args.IsSuccess(&mut ok)?;
            let mut status = COREWEBVIEW2_WEB_ERROR_STATUS::default();
            args.WebErrorStatus(&mut status)?;
            if !ok.as_bool() && status != COREWEBVIEW2_WEB_ERROR_STATUS_OPERATION_CANCELED {
                let _ = app.emit_to(
                    crate::chrome::MAIN,
                    super::GUEST_EVENT,
                    &GuestEvent::Fail {
                        label: label.clone(),
                        code: i64::from(status.0),
                        description: format!(
                            "The page could not be loaded (WebView2 error {}).",
                            status.0
                        ),
                    },
                );
            }
            Ok(())
        }));
        let mut token = Default::default();
        let _ = core.add_NavigationCompleted(&handler, &mut token);
        let focused = webview2_com::FocusChangedEventHandler::create(Box::new(move |_, _| {
            let _ = focus_app.emit_to(
                crate::chrome::MAIN,
                super::GUEST_EVENT,
                &GuestEvent::Focus {
                    label: focus_label.clone(),
                },
            );
            Ok(())
        }));
        let _ = view.controller().add_GotFocus(&focused, &mut token);
        // WebView2 asks before the page does, so the key is only noted here
        // and left for the page; its init script says when the page did not
        // handle it.
        let keys = AcceleratorKeyPressedEventHandler::create(Box::new(move |_, args| {
            let Some(args) = args else { return Ok(()) };
            if let Some((key, repeat)) = menu_key(&args)? {
                let action = crate::app_menu::key_action(&keys_app, key.as_pressed());
                super::pending_keys(&keys_app).press(&keys_label, key.key, action, repeat);
            }
            Ok(())
        }));
        let _ = view
            .controller()
            .add_AcceleratorKeyPressed(&keys, &mut token);
        // Only the top frame's messages arrive here.
        let said = WebMessageReceivedEventHandler::create(Box::new(move |_, args| {
            let Some(args) = args else { return Ok(()) };
            let mut message = windows::core::PWSTR::null();
            if args.TryGetWebMessageAsString(&mut message).is_err() {
                return Ok(());
            }
            let message = webview2_com::take_pwstr(message);
            super::unhandled_key(&said_app, &said_label, &message);
            Ok(())
        }));
        let _ = core.add_WebMessageReceived(&said, &mut token);
    });
}

/// A key the guest is about to get, owned for [`menu_key`]'s caller.
struct Key {
    key: String,
    ctrl: bool,
    shift: bool,
    alt: bool,
    meta: bool,
}

impl Key {
    fn as_pressed(&self) -> agentrq_desktop_core::menu_keys::Pressed<'_> {
        // A virtual key is already the Latin letter on a layout of another
        // script, so no place is needed.
        agentrq_desktop_core::menu_keys::Pressed {
            key: &self.key,
            code: None,
            ctrl: self.ctrl,
            shift: self.shift,
            alt: self.alt,
            meta: self.meta,
        }
    }
}

/// A key down in the guest that could be a menu key, and whether it is a
/// repeat of one already down.
fn menu_key(
    args: &webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2AcceleratorKeyPressedEventArgs,
) -> windows::core::Result<Option<(Key, bool)>> {
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        COREWEBVIEW2_KEY_EVENT_KIND, COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN,
        COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN, COREWEBVIEW2_PHYSICAL_KEY_STATUS,
    };
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::{
        GetKeyState, VK_CONTROL, VK_LWIN, VK_MENU, VK_RWIN, VK_SHIFT,
    };
    let mut kind = COREWEBVIEW2_KEY_EVENT_KIND::default();
    unsafe { args.KeyEventKind(&mut kind)? };
    if kind != COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN
        && kind != COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN
    {
        return Ok(None);
    }
    let down = |key: u16| unsafe { GetKeyState(i32::from(key)) } < 0;
    let shift = down(VK_SHIFT);
    let mut vk = 0u32;
    unsafe { args.VirtualKey(&mut vk)? };
    let Some(key) = agentrq_desktop_core::menu_keys::windows_key(vk, shift) else {
        return Ok(None);
    };
    let mut status = COREWEBVIEW2_PHYSICAL_KEY_STATUS::default();
    unsafe { args.PhysicalKeyStatus(&mut status)? };
    Ok(Some((
        Key {
            key,
            ctrl: down(VK_CONTROL),
            shift,
            alt: down(VK_MENU),
            meta: down(VK_LWIN) || down(VK_RWIN),
        },
        status.WasKeyDown.as_bool(),
    )))
}

pub fn held() -> Modifiers {
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::{GetKeyState, VK_CONTROL, VK_MBUTTON};
    let down = |key: u16| unsafe { GetKeyState(i32::from(key)) } < 0;
    Modifiers {
        control: down(VK_CONTROL),
        meta: false,
        middle: down(VK_MBUTTON),
    }
}

pub async fn history<R: Runtime>(guest: &Webview<R>) -> (bool, bool) {
    let (tx, rx) = tokio::sync::oneshot::channel();
    let _ = guest.with_webview(move |view| unsafe {
        let mut back = windows::core::BOOL::default();
        let mut forward = windows::core::BOOL::default();
        if let Ok(core) = view.controller().CoreWebView2() {
            let _ = core.CanGoBack(&mut back);
            let _ = core.CanGoForward(&mut forward);
        }
        let _ = tx.send((back.as_bool(), forward.as_bool()));
    });
    rx.await.unwrap_or_default()
}

pub fn go<R: Runtime>(guest: &Webview<R>, step: i32) {
    let _ = guest.with_webview(move |view| unsafe {
        if let Ok(core) = view.controller().CoreWebView2() {
            let _ = if step < 0 {
                core.GoBack()
            } else {
                core.GoForward()
            };
        }
    });
}

pub async fn snapshot<R: Runtime>(guest: &Webview<R>) -> Result<Vec<u8>, String> {
    use webview2_com::CapturePreviewCompletedHandler;
    use webview2_com::Microsoft::Web::WebView2::Win32::COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG;
    use windows::Win32::System::Com::{IStream, STATFLAG_NONAME, STATSTG, STREAM_SEEK_SET};
    use windows::Win32::UI::Shell::SHCreateMemStream;

    let (tx, rx) = tokio::sync::oneshot::channel::<Result<Vec<u8>, String>>();
    guest
        .with_webview(move |view| unsafe {
            let tx = std::sync::Mutex::new(Some(tx));
            let fail = |e: String| {
                if let Some(tx) = tx.lock().unwrap().take() {
                    let _ = tx.send(Err(e));
                }
            };
            let core = match view.controller().CoreWebView2() {
                Ok(core) => core,
                Err(e) => return fail(e.to_string()),
            };
            let Some(stream) = SHCreateMemStream(None) else {
                return fail("no stream".to_string());
            };
            let read: IStream = stream.clone();
            let tx = std::sync::Arc::new(tx);
            let done = std::sync::Arc::clone(&tx);
            let handler = CapturePreviewCompletedHandler::create(Box::new(move |result| {
                let png = (|| -> windows::core::Result<Vec<u8>> {
                    result?;
                    let mut stat = STATSTG::default();
                    read.Stat(&mut stat, STATFLAG_NONAME)?;
                    read.Seek(0, STREAM_SEEK_SET, None)?;
                    let mut png = vec![0u8; stat.cbSize as usize];
                    let mut got = 0u32;
                    read.Read(png.as_mut_ptr().cast(), png.len() as u32, Some(&mut got))
                        .ok()?;
                    png.truncate(got as usize);
                    Ok(png)
                })();
                if let Some(tx) = done.lock().unwrap().take() {
                    let _ = tx.send(png.map_err(|e| e.to_string()));
                }
                Ok(())
            }));
            if let Err(e) = core.CapturePreview(
                COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG,
                &stream,
                &handler,
            ) {
                if let Some(tx) = tx.lock().unwrap().take() {
                    let _ = tx.send(Err(e.to_string()));
                }
            }
        })
        .map_err(|e| e.to_string())?;
    super::within(rx).await
}
