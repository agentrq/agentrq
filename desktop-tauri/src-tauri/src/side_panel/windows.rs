// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What WebView2 is asked directly for the side panel's guest, because Tauri
//! does not. Native glue, outside the coverage gate: the decisions are
//! `core`'s and `side_panel.rs`'s.

use std::cell::{Cell, RefCell};
use std::collections::HashMap;
use std::path::PathBuf;

use agentrq_desktop_core::side_panel::child::{Modifiers, Placement};
use tauri::webview::WebviewBuilder;
use tauri::{LogicalPosition, LogicalSize, Runtime, Webview};

use webview2_com::Microsoft::Web::WebView2::Win32::{
    ICoreWebView2Deferral, ICoreWebView2PermissionRequestedEventArgs,
};

thread_local! {
    /// The permission requests a guest's page is waiting on, each with the
    /// deferral that keeps WebView2 waiting, until the broker answers.
    static WAITING: RefCell<HashMap<u64, (ICoreWebView2PermissionRequestedEventArgs, ICoreWebView2Deferral)>> =
        RefCell::new(HashMap::new());
    static NEXT_REQUEST: Cell<u64> = const { Cell::new(1) };
}

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

/// A page's permission request, held by a deferral until the broker answers
/// it on the main thread. WebView2 neither prompts nor remembers: the
/// window asks, and the profile's own file remembers.
fn watch_permissions<R: Runtime>(
    core: &webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2,
    app: tauri::AppHandle<R>,
    label: String,
) {
    use agentrq_desktop_core::side_panel::platform_permissions::webview2_ask;
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        ICoreWebView2PermissionRequestedEventArgs3, COREWEBVIEW2_PERMISSION_KIND,
    };
    use webview2_com::PermissionRequestedEventHandler;
    let handler = PermissionRequestedEventHandler::create(Box::new(move |_, args| {
        let Some(args) = args else { return Ok(()) };
        let mut kind = COREWEBVIEW2_PERMISSION_KIND::default();
        let mut uri = windows::core::PWSTR::null();
        unsafe {
            args.PermissionKind(&mut kind)?;
            args.Uri(&mut uri)?;
        }
        let uri = webview2_com::take_pwstr(uri);
        if let Ok(args3) =
            windows::core::Interface::cast::<ICoreWebView2PermissionRequestedEventArgs3>(&args)
        {
            let _ = unsafe { args3.SetSavesInProfile(false) };
        }
        let deferral = unsafe { args.GetDeferral()? };
        let id = NEXT_REQUEST.get();
        NEXT_REQUEST.set(id + 1);
        WAITING.with(|waiting| waiting.borrow_mut().insert(id, (args, deferral)));
        let main = app.clone();
        crate::panel_permissions::request(
            &app,
            &label,
            &uri,
            webview2_ask(kind.0, &uri),
            Box::new(move |granted| {
                let _ = main.run_on_main_thread(move || settle(id, granted));
            }),
        );
        Ok(())
    }));
    let mut token = Default::default();
    let _ = unsafe { core.add_PermissionRequested(&handler, &mut token) };
}

fn settle(id: u64, granted: bool) {
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        COREWEBVIEW2_PERMISSION_STATE_ALLOW, COREWEBVIEW2_PERMISSION_STATE_DENY,
    };
    let Some((args, deferral)) = WAITING.with(|waiting| waiting.borrow_mut().remove(&id)) else {
        return;
    };
    let state = if granted {
        COREWEBVIEW2_PERMISSION_STATE_ALLOW
    } else {
        COREWEBVIEW2_PERMISSION_STATE_DENY
    };
    unsafe {
        let _ = args.SetState(state);
        let _ = deferral.Complete();
    }
}

/// WebView2's own find, from runtime 1.0.3240: it counts and says which
/// match is active. `None` on an older runtime.
fn finder(
    core: &webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2,
) -> Option<webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2Find> {
    use webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2_28;
    let core = windows::core::Interface::cast::<ICoreWebView2_28>(core).ok()?;
    unsafe { core.Find() }.ok()
}

/// What WebView2's find counts, for the window's find bar.
fn watch_find<R: Runtime>(
    core: &webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2,
    app: tauri::AppHandle<R>,
    label: String,
) {
    use webview2_com::{
        FindActiveMatchIndexChangedEventHandler, FindMatchCountChangedEventHandler,
    };
    let Some(find) = finder(core) else {
        return;
    };
    let report =
        move |find: Option<webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2Find>| {
            let Some(find) = find else { return Ok(()) };
            let (mut index, mut count) = (0i32, 0i32);
            unsafe {
                find.ActiveMatchIndex(&mut index)?;
                find.MatchCount(&mut count)?;
            }
            let found = agentrq_desktop_core::side_panel::find::webview2_found(index, count);
            super::report_found(&app, &label, found);
            Ok(())
        };
    let report = std::sync::Arc::new(report);
    let on_index = std::sync::Arc::clone(&report);
    let mut token = Default::default();
    unsafe {
        let _ = find.add_ActiveMatchIndexChanged(
            &FindActiveMatchIndexChangedEventHandler::create(Box::new(move |find, _| {
                (*on_index)(find)
            })),
            &mut token,
        );
        let _ = find.add_MatchCountChanged(
            &FindMatchCountChangedEventHandler::create(Box::new(move |find, _| (*report)(find))),
            &mut token,
        );
    }
}

pub fn find<R: Runtime>(
    guest: &Webview<R>,
    text: &str,
    step: agentrq_desktop_core::side_panel::find::FindStep,
) {
    use agentrq_desktop_core::side_panel::find::{find_script, FindStep};
    use tauri::Manager;
    use webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2Environment15;
    use webview2_com::{ExecuteScriptCompletedHandler, FindStartCompletedHandler};
    let text = text.to_string();
    let app = guest.app_handle().clone();
    let label = guest.label().to_string();
    let _ = guest.with_webview(move |view| unsafe {
        let Ok(core) = view.controller().CoreWebView2() else {
            return;
        };
        if let Some(find) = finder(&core) {
            let _ = match step {
                FindStep::Start => {
                    let Ok(options) = windows::core::Interface::cast::<ICoreWebView2Environment15>(
                        &view.environment(),
                    )
                    .and_then(|env| env.CreateFindOptions()) else {
                        return;
                    };
                    let _ = options.SetFindTerm(&windows::core::HSTRING::from(text.as_str()));
                    let _ = options.SetIsCaseSensitive(false);
                    let _ = options.SetShouldHighlightAllMatches(true);
                    find.Start(
                        &options,
                        &FindStartCompletedHandler::create(Box::new(|_| Ok(()))),
                    )
                }
                FindStep::Next => find.FindNext(),
                FindStep::Previous => find.FindPrevious(),
            };
            return;
        }
        // An older runtime: the page's own `window.find`, counted by script.
        let script = windows::core::HSTRING::from(find_script(&text, step));
        let _ = core.ExecuteScript(
            &script,
            &ExecuteScriptCompletedHandler::create(Box::new(move |_, result| {
                let total = result.trim().parse::<u32>().unwrap_or(0);
                super::counted(&app, &label, total);
                Ok(())
            })),
        );
    });
}

pub fn stop_find<R: Runtime>(guest: &Webview<R>, clear: bool) {
    let _ = guest.with_webview(move |view| unsafe {
        if let Some(find) = view
            .controller()
            .CoreWebView2()
            .ok()
            .as_ref()
            .and_then(finder)
        {
            let _ = find.Stop();
        }
    });
    if clear {
        let _ = guest.eval(agentrq_desktop_core::side_panel::find::CLEAR_SELECTION_SCRIPT);
    }
}

/// A navigation that did not complete, for the panel's own error view; the
/// guest taking focus; the menu keys its page leaves unhandled, and its find
/// key, which the page never sees; its find counts and permission requests.
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
    let (perm_app, perm_label) = (app.clone(), label.clone());
    let (find_app, find_label) = (app.clone(), label.clone());
    let _ = guest.with_webview(move |view| unsafe {
        let Ok(core) = view.controller().CoreWebView2() else {
            return;
        };
        watch_permissions(&core, perm_app, perm_label);
        watch_find(&core, find_app, find_label);
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
        // handle it. Any other key down lets the noted one go.
        let keys = AcceleratorKeyPressedEventHandler::create(Box::new(move |_, args| {
            let Some(args) = args else { return Ok(()) };
            if !key_down(&args)? {
                return Ok(());
            }
            let pending = super::pending_keys(&keys_app);
            match menu_key(&args)? {
                Some((key, _)) if super::key_down(&keys_app, &key.as_input()) => {
                    args.SetHandled(true)?;
                    pending.forget(&keys_label);
                }
                Some((key, repeat)) => {
                    let action = crate::app_menu::key_action(&keys_app, key.as_pressed());
                    pending.press(&keys_label, key.key, action, repeat);
                }
                None => pending.forget(&keys_label),
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
    fn as_input(&self) -> agentrq_desktop_core::side_panel::wire::KeyInput {
        agentrq_desktop_core::side_panel::wire::KeyInput {
            kind: "keyDown".to_string(),
            key: self.key.clone(),
            shift: self.shift,
            alt: self.alt,
            control: self.ctrl,
            meta: self.meta,
        }
    }

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

/// Whether the event is a key going down.
fn key_down(
    args: &webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2AcceleratorKeyPressedEventArgs,
) -> windows::core::Result<bool> {
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        COREWEBVIEW2_KEY_EVENT_KIND, COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN,
        COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN,
    };
    let mut kind = COREWEBVIEW2_KEY_EVENT_KIND::default();
    unsafe { args.KeyEventKind(&mut kind)? };
    Ok(kind == COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN
        || kind == COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN)
}

/// The menu key a key down in the guest could be, and whether it is a
/// repeat of one already down.
fn menu_key(
    args: &webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2AcceleratorKeyPressedEventArgs,
) -> windows::core::Result<Option<(Key, bool)>> {
    use webview2_com::Microsoft::Web::WebView2::Win32::COREWEBVIEW2_PHYSICAL_KEY_STATUS;
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::{
        GetKeyState, VK_CONTROL, VK_LWIN, VK_MENU, VK_RWIN, VK_SHIFT,
    };
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
