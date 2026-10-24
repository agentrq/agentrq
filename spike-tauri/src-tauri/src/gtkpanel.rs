// WP0 S6 on Linux: Tauri packs a child webview into the window's GtkBox, so
// it is stacked under the page instead of placed at its rect. Move both
// webviews into a GtkOverlay and place the guest from the rect. Never merge.
use gtk::prelude::*;
use std::cell::{Cell, RefCell};

thread_local! {
    static RECT: Cell<(i32, i32, i32, i32)> = const { Cell::new((0, 0, 0, 0)) };
    static MAIN: RefCell<Option<gtk::Widget>> = const { RefCell::new(None) };
    static OVERLAY: RefCell<Option<gtk::Overlay>> = const { RefCell::new(None) };
}

pub fn set_main(main: gtk::Widget) {
    MAIN.with(|m| *m.borrow_mut() = Some(main));
}

pub fn adopt(vbox: gtk::Box, panel: gtk::Widget, rect: (i32, i32, i32, i32)) {
    RECT.with(|r| r.set(rect));
    let main = MAIN.with(|m| m.borrow().clone()).expect("main webview first");
    vbox.remove(&main);
    vbox.remove(&panel);
    let overlay = gtk::Overlay::new();
    overlay.add(&main);
    overlay.add_overlay(&panel);
    overlay.connect_get_child_position(|_, _| {
        let (x, y, w, h) = RECT.with(|r| r.get());
        Some(gtk::gdk::Rectangle::new(x, y, w, h))
    });
    vbox.pack_start(&overlay, true, true, 0);
    overlay.show_all();
    OVERLAY.with(|o| *o.borrow_mut() = Some(overlay));
}

pub fn set_rect(rect: (i32, i32, i32, i32)) {
    RECT.with(|r| r.set(rect));
    OVERLAY.with(|o| {
        if let Some(o) = o.borrow().as_ref() {
            o.queue_resize();
        }
    });
}
