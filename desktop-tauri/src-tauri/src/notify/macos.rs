// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! macOS: the shell's notifications, through `NSUserNotificationCenter` with
//! a delegate of its own.
//!
//! Not tauri-plugin-notification's: notify-rust waits for the click on a
//! thread of its own, and on macOS 27 it gives up about a second after
//! posting, so a click on the banner only focused the app. The delegate here
//! is called for every click for as long as the app runs, and the route rides
//! in the notification's `userInfo`. `UNUserNotificationCenter` would need a
//! Developer ID signature; an ad-hoc build is refused outright.
#![allow(deprecated)]

use std::sync::OnceLock;

use objc2::rc::Retained;
use objc2::runtime::{AnyObject, NSObject, NSObjectProtocol, ProtocolObject};
use objc2::{define_class, msg_send, ClassType, MainThreadMarker, MainThreadOnly};
use objc2_foundation::{
    NSDictionary, NSString, NSUserNotification, NSUserNotificationCenter,
    NSUserNotificationCenterDelegate,
};

use super::ROUTE_KEY;

type OnClick = Box<dyn Fn(String) + Send + Sync>;

/// What a click on one of the shell's notifications does with its route.
static ON_CLICK: OnceLock<OnClick> = OnceLock::new();

define_class!(
    // SAFETY: NSObject has no subclassing requirements, and `Clicks` does not
    // implement `Drop`.
    #[unsafe(super(NSObject))]
    #[thread_kind = MainThreadOnly]
    #[name = "AgentRQNotificationClicks"]
    struct Clicks;

    unsafe impl NSObjectProtocol for Clicks {}

    unsafe impl NSUserNotificationCenterDelegate for Clicks {
        #[unsafe(method(userNotificationCenter:didActivateNotification:))]
        fn activated(&self, center: &NSUserNotificationCenter, notification: &NSUserNotification) {
            if let (Some(route), Some(on_click)) = (route_of(notification), ON_CLICK.get()) {
                on_click(route);
            }
            center.removeDeliveredNotification(notification);
        }
    }
);

impl Clicks {
    fn new(mtm: MainThreadMarker) -> Retained<Self> {
        let this = Self::alloc(mtm).set_ivars(());
        unsafe { msg_send![super(this), init] }
    }
}

/// The process's center; `None` for a binary run outside its bundle.
fn center() -> Option<Retained<NSUserNotificationCenter>> {
    unsafe {
        msg_send![
            NSUserNotificationCenter::class(),
            defaultUserNotificationCenter
        ]
    }
}

fn route_of(notification: &NSUserNotification) -> Option<String> {
    let info = notification.userInfo()?;
    let route = info.objectForKey(&NSString::from_str(ROUTE_KEY))?;
    Some(route.downcast::<NSString>().ok()?.to_string())
}

/// Send each clicked notification's route to `on_click`. Once, on the main
/// thread, before the first notification.
pub fn watch(mtm: MainThreadMarker, on_click: impl Fn(String) + Send + Sync + 'static) {
    if ON_CLICK.set(Box::new(on_click)).is_err() {
        return;
    }
    let Some(center) = center() else {
        return;
    };
    // The center keeps its delegate weakly.
    let clicks = Box::leak(Box::new(Clicks::new(mtm)));
    unsafe { center.setDelegate(Some(ProtocolObject::from_ref(&**clicks))) };
}

/// Post a notification that carries `route`; on the main thread. A binary
/// run outside its bundle has no center, and posts nothing.
pub fn show(title: &str, body: &str, route: &str) {
    if let Some(center) = center() {
        center.deliverNotification(&notification(title, body, route));
    }
}

fn notification(title: &str, body: &str, route: &str) -> Retained<NSUserNotification> {
    let notification = NSUserNotification::new();
    notification.setTitle(Some(&NSString::from_str(title)));
    notification.setInformativeText(Some(&NSString::from_str(body)));
    let route: Retained<AnyObject> = NSString::from_str(route).into();
    let info = NSDictionary::from_retained_objects(&[&*NSString::from_str(ROUTE_KEY)], &[route]);
    unsafe { notification.setUserInfo(Some(&info)) };
    notification
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_notification_carries_its_route_to_the_click() {
        let posted = notification("ALPHA", "A reply", "/workspaces/w/tasks/t");
        assert_eq!(route_of(&posted).as_deref(), Some("/workspaces/w/tasks/t"));
        assert_eq!(
            posted.title().map(|t| t.to_string()).as_deref(),
            Some("ALPHA")
        );
        // Not one of the shell's: nothing to open.
        assert_eq!(route_of(&NSUserNotification::new()), None);
    }
}
