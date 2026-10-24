// WP0 spike S6, mitigation (a): a PNG of a webview from the platform's own
// snapshot API. Never merge.
use tauri::webview::PlatformWebview;
use tokio::sync::oneshot::Sender;

pub type Result = std::result::Result<Vec<u8>, String>;

#[cfg(target_os = "linux")]
pub fn take(pv: PlatformWebview, tx: Sender<Result>) {
    use webkit2gtk::{SnapshotOptions, SnapshotRegion, WebViewExt};
    let wv = pv.inner();
    wv.snapshot(
        SnapshotRegion::Visible,
        SnapshotOptions::NONE,
        None::<&gio::Cancellable>,
        move |res| {
            let out = res.map_err(|e| e.to_string()).and_then(|surface| {
                let img = cairo::ImageSurface::try_from(surface).map_err(|_| "not an image surface".to_string())?;
                let mut buf = Vec::new();
                img.write_to_png(&mut buf).map_err(|e| e.to_string())?;
                Ok(buf)
            });
            let _ = tx.send(out);
        },
    );
}

#[cfg(target_os = "macos")]
pub fn take(pv: PlatformWebview, tx: Sender<Result>) {
    use block2::RcBlock;
    use objc2::MainThreadMarker;
    use objc2_app_kit::{NSBitmapImageFileType, NSBitmapImageRep, NSImage};
    use objc2_foundation::{NSDictionary, NSError};
    use objc2_web_kit::{WKSnapshotConfiguration, WKWebView};
    use std::cell::RefCell;

    let wk: &WKWebView = unsafe { &*(pv.inner() as *const WKWebView) };
    let mtm = MainThreadMarker::new().expect("main thread");
    let cfg = unsafe { WKSnapshotConfiguration::new(mtm) };
    let tx = RefCell::new(Some(tx));
    let block = RcBlock::new(move |img: *mut NSImage, _err: *mut NSError| {
        let out = if img.is_null() {
            Err("takeSnapshot returned no image".to_string())
        } else {
            let img = unsafe { &*img };
            img.TIFFRepresentation()
                .and_then(|tiff| NSBitmapImageRep::imageRepWithData(&tiff))
                .and_then(|rep| unsafe {
                    rep.representationUsingType_properties(NSBitmapImageFileType::PNG, &NSDictionary::new())
                })
                .map(|png| png.to_vec())
                .ok_or_else(|| "PNG conversion failed".to_string())
        };
        if let Some(tx) = tx.borrow_mut().take() {
            let _ = tx.send(out);
        }
    });
    unsafe { wk.takeSnapshotWithConfiguration_completionHandler(Some(&cfg), &block) };
}

#[cfg(windows)]
pub fn take(pv: PlatformWebview, tx: Sender<Result>) {
    use webview2_com::CapturePreviewCompletedHandler;
    use webview2_com::Microsoft::Web::WebView2::Win32::COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG;
    use windows::Win32::System::Com::{IStream, STATFLAG_NONAME, STATSTG, STREAM_SEEK_SET};
    use windows::Win32::UI::Shell::SHCreateMemStream;

    let run = || -> windows::core::Result<()> {
        unsafe {
            let wv = pv.controller().CoreWebView2()?;
            let stream: IStream = SHCreateMemStream(None).ok_or_else(windows::core::Error::from_win32)?;
            let s2 = stream.clone();
            let handler = CapturePreviewCompletedHandler::create(Box::new(move |res| {
                let out = (|| -> windows::core::Result<Vec<u8>> {
                    res?;
                    let mut stat = STATSTG::default();
                    s2.Stat(&mut stat, STATFLAG_NONAME)?;
                    s2.Seek(0, STREAM_SEEK_SET, None)?;
                    let mut buf = vec![0u8; stat.cbSize as usize];
                    let mut read = 0u32;
                    s2.Read(buf.as_mut_ptr() as _, buf.len() as u32, Some(&mut read)).ok()?;
                    buf.truncate(read as usize);
                    Ok(buf)
                })();
                let _ = tx.send(out.map_err(|e| e.to_string()));
                Ok(())
            }));
            wv.CapturePreview(COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG, &stream, &handler)
        }
    };
    if let Err(e) = run() {
        eprintln!("[spike] CapturePreview failed: {e}");
    }
}
