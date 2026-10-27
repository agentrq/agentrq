// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The supervisor's authorisation window (Electron's `openAuthorizationWindow`).
//!
//! It shows the server's authorise page on the profile's webview storage, never
//! the side panel's, and ends at the redirect, which is cancelled before it can
//! reach the OS or the deep-link handler (`core::oauth_window`). One is open at
//! a time.
//!
//! The authorise page reads the `at` cookie, which is the profile's jar's, not
//! the webview's: Electron's window has it from the partition. So the jar's
//! cookies for the page are lent to the window for its run, as session cookies,
//! and every cookie for that host is taken back before it closes, as a sign-in
//! window's are: no session is left in a webview store.

mod handlers;

use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use agentrq_desktop_core::auth::cookie_domain_matches;
pub use agentrq_desktop_core::oauth_window::OAuthError;
use agentrq_desktop_core::oauth_window::{Flow, DEADLINE};
use cookie_store::RawCookie;
use serde_json::Value;
use tauri::webview::cookie::SameSite;
use tauri::{AppHandle, Manager, Runtime, WebviewWindow, WindowEvent};
use tokio::sync::{oneshot, Notify};
use url::Url;

use crate::cookie_jar::ProfileJar;
use crate::network::{Network, ProfileNet};
use crate::profiles::Profiles;
use crate::sign_in::{self, CookieWebview, COOKIE_DEADLINE};

/// Windows are numbered, like sign-in windows, so a closing one never frees a
/// label another uses. None is `main`, so none gets a capability.
static WINDOWS: AtomicU32 = AtomicU32::new(1);

/// The longest a closing window spends taking its session back. A webview
/// that never answers would otherwise hold the window open, and the slot,
/// for the rest of the session; what was lent is a session cookie, so a
/// missed take-back costs less than that.
const TAKE_BACK_DEADLINE: Duration = Duration::from_secs(2 * COOKIE_DEADLINE.as_secs());

type Ended = Result<Url, OAuthError>;

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

/// The authorisation under way, if any: one at a time. A run's [`Lease`]
/// holds a handle to it, to free it however the run stops.
#[derive(Default, Clone)]
pub struct OAuthWindows {
    open: Arc<Mutex<Option<Slot>>>,
    freed: Arc<Notify>,
    /// How many sign-in windows each profile's store has open.
    signing_in: Arc<Mutex<HashMap<String, usize>>>,
}

/// The window's label, the profile whose store it shows, and its flow.
type Slot = (String, String, Arc<Pending>);

impl OAuthWindows {
    /// Takes the slot for `label`, or says why not: another window has it,
    /// named, or a sign-in window is open on `profile`'s store.
    fn begin(
        &self,
        label: &str,
        profile: &str,
        pending: Arc<Pending>,
    ) -> Result<(), Option<String>> {
        let mut open = lock(&self.open);
        if let Some((other, _, _)) = &*open {
            return Err(Some(other.clone()));
        }
        if lock(&self.signing_in).contains_key(profile) {
            return Err(None);
        }
        *open = Some((label.to_string(), profile.to_string(), pending));
        Ok(())
    }

    /// Readies `profile`'s store for a sign-in window: ends the authorisation
    /// window open on it, if one is, and keeps any new one from opening
    /// there until the [`SignIn`] is dropped. Held first, so that none opens
    /// in between.
    pub async fn sign_in(&self, profile: &str) -> SignIn {
        *lock(&self.signing_in)
            .entry(profile.to_string())
            .or_default() += 1;
        let held = SignIn {
            windows: self.clone(),
            profile: profile.to_string(),
        };
        self.close_for(profile).await;
        held
    }

    fn finish(&self) {
        *lock(&self.open) = None;
        self.freed.notify_waiters();
    }

    /// Ends the window open on `profile`'s store, if one is, as a close
    /// would, and waits until it has taken its session back and gone.
    ///
    /// Asked before that store is deleted with its profile, and before a
    /// sign-in opens on it: the sign-in would read the lent `at` as its own,
    /// and the take-back would then delete the session the sign-in set.
    pub async fn close_for(&self, profile: &str) {
        let open = lock(&self.open).clone();
        let Some((_, _, pending)) = open.filter(|(_, on, _)| on == profile) else {
            return;
        };
        pending.end(OAuthError::Closed);
        loop {
            // Made before the check: it hears a `finish` that comes between.
            let freed = self.freed.notified();
            let still = lock(&self.open)
                .as_ref()
                .is_some_and(|(_, _, now)| Arc::ptr_eq(now, &pending));
            if !still {
                return;
            }
            freed.await;
        }
    }

    #[cfg(test)]
    fn current(&self) -> Option<Slot> {
        lock(&self.open).clone()
    }
}

/// A sign-in window open on a profile's store, held for as long as it is:
/// no authorisation window opens on that store meanwhile.
pub struct SignIn {
    windows: OAuthWindows,
    profile: String,
}

impl SignIn {
    /// The profile whose store it holds: the one its window must show.
    pub fn profile(&self) -> &str {
        &self.profile
    }
}

impl Drop for SignIn {
    fn drop(&mut self) {
        let mut signing_in = lock(&self.windows.signing_in);
        let count = signing_in.entry(self.profile.clone()).or_default();
        *count = count.saturating_sub(1);
        if *count == 0 {
            signing_in.remove(&self.profile);
        }
    }
}

/// A [`SignIn`] its window keeps, until that window is destroyed.
pub struct HeldSignIn(Mutex<Option<SignIn>>);

impl HeldSignIn {
    pub fn new(held: SignIn) -> Self {
        Self(Mutex::new(Some(held)))
    }

    /// Each of the window's events: once it is destroyed, the hold ends.
    pub fn on_window_event(&self, event: &WindowEvent) {
        if let WindowEvent::Destroyed = event {
            lock(&self.0).take();
        }
    }
}

/// What opens a sign-in window, handed the start URL and the [`SignIn`] to
/// hold until that window is gone. The window shows the store of
/// [`SignIn::profile`], and no other.
pub type SignInOpener<R> = Box<dyn FnOnce(&AppHandle<R>, String, SignIn) + Send>;

/// Opens a sign-in window on the active profile's store with `open`, once the
/// authorisation window open there, if any, has taken its session back and
/// gone. Otherwise the sign-in would read the lent `at` as its own, and the
/// take-back would delete the session the sign-in set; for the same reason
/// no authorisation opens on that store until the sign-in window is gone.
pub fn open_sign_in<R: Runtime>(app: &AppHandle<R>, start_url: String, open: SignInOpener<R>) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let profiles = app.state::<Arc<Profiles>>().inner().clone();
        let profile = profiles.active_id();
        let held = profiles.auth_windows().sign_in(&profile).await;
        // Another profile was switched to during the take-back. The sign-in
        // was asked for the one shown then, which the window no longer shows.
        if profiles.active_id() != profile {
            eprintln!("[sign-in] another profile was opened meanwhile; this sign-in is dropped");
            return;
        }
        open(&app, start_url, held);
    });
}

/// One window's flow, and where its ending goes.
pub struct Pending {
    flow: Mutex<Flow>,
    done: Mutex<Option<oneshot::Sender<Ended>>>,
    /// Set just before the run destroys the window. Until then a close waits,
    /// so the window's session is always taken back first.
    destroying: AtomicBool,
}

impl Pending {
    /// Whether the window may follow `url`; the redirect ends the flow here.
    pub fn navigate(&self, url: &Url) -> bool {
        let step = lock(&self.flow).navigate(url);
        if let Some(ended) = step.ended {
            self.send(ended);
        }
        step.load
    }

    /// A subframe's navigation, which WebView2 reports as text: it never ends
    /// the flow, and one that does not parse is refused.
    #[cfg_attr(not(windows), allow(dead_code))]
    pub fn navigate_frame(&self, uri: &str) -> bool {
        Url::parse(uri).is_ok_and(|url| lock(&self.flow).navigate_frame(&url))
    }

    /// Ends the flow for `why`; whether this was what ended it.
    pub fn end(&self, why: OAuthError) -> bool {
        let ended = lock(&self.flow).end(why);
        ended.map(|ended| self.send(ended)).is_some()
    }

    /// The person closing the window, which is declining. Whether to keep it
    /// open: always, until the run has taken its session back and destroys
    /// it, which no close may skip. Read before ending: the run it wakes may
    /// be destroying the window by the time `end` returns.
    pub fn close_requested(&self) -> bool {
        let held = !self.destroying.load(Ordering::SeqCst);
        self.end(OAuthError::Closed);
        held
    }

    fn send(&self, ended: Ended) {
        if let Some(done) = lock(&self.done).take() {
            let _ = done.send(ended);
        }
    }
}

/// The window's cookie store: what a sign-in reads and deletes, and the call
/// that lends it the profile's session. All of them wait on the webview, so
/// they are made on a blocking thread (wry#583).
pub trait SessionWebview: CookieWebview {
    fn set_cookie(&self, cookie: RawCookie<'static>);
}

/// The window as a run drives it: lent its session, shown the page, then
/// destroyed once the session is back.
trait AuthWindow: SessionWebview {
    fn navigate(&self, url: Url) -> Result<(), OAuthError>;
    fn destroy(&self);
}

struct TauriWindow<R: Runtime>(WebviewWindow<R>);

impl<R: Runtime> CookieWebview for TauriWindow<R> {
    fn cookies(&self) -> Vec<RawCookie<'static>> {
        self.0.cookies().unwrap_or_default()
    }
    fn delete_cookie(&self, cookie: RawCookie<'static>) {
        let _ = self.0.delete_cookie(cookie);
    }
}

impl<R: Runtime> SessionWebview for TauriWindow<R> {
    fn set_cookie(&self, cookie: RawCookie<'static>) {
        let _ = self.0.set_cookie(cookie);
    }
}

impl<R: Runtime> AuthWindow for TauriWindow<R> {
    fn navigate(&self, url: Url) -> Result<(), OAuthError> {
        self.0.navigate(url).map_err(window_error)
    }
    fn destroy(&self) {
        // Not `close()`: that asks the window again.
        let _ = self.0.destroy();
    }
}

/// The jar's cookies for `page`, as the window keeps them: for `host` only,
/// and for this session, so a store never writes them to disk. `SameSite` is
/// the server's, Lax when it set none: WebKit reads none as `None`, which
/// would let any page in the window post to the server as the person.
fn lent(jar: &ProfileJar, page: &Url, host: &str) -> Vec<RawCookie<'static>> {
    let secure = page.scheme() == "https";
    jar.sent_to(page)
        .into_iter()
        .map(|sent| {
            let mut cookie = RawCookie::new(sent.name().to_string(), sent.value().to_string());
            cookie.set_domain(host.to_string());
            cookie.set_path("/");
            cookie.set_http_only(true);
            cookie.set_secure(secure);
            cookie.set_same_site(sent.same_site().unwrap_or(SameSite::Lax));
            cookie
        })
        .collect()
}

/// Lends `cookies` to the window. tauri-runtime-wry sends each `set_cookie`
/// to the event loop without waiting, so this returns at once; the cookies
/// still land before the page loads, because the navigate after them is a
/// later message on the same loop (on macOS wry's `set_cookie` holds the loop
/// until WKHTTPCookieStore is done). The deadline is for a runtime that waits.
async fn lend(webview: Arc<dyn SessionWebview>, cookies: Vec<RawCookie<'static>>) {
    let job = tokio::task::spawn_blocking(move || {
        for cookie in cookies {
            webview.set_cookie(cookie);
        }
    });
    let _ = tokio::time::timeout(COOKIE_DEADLINE, job).await;
}

/// Takes back what was lent, and any session the window got for `host` by a
/// sign-in inside it.
async fn take_back(webview: Arc<dyn SessionWebview>, lent: Vec<RawCookie<'static>>, host: &str) {
    let (found, _) = sign_in::read(webview.clone(), COOKIE_DEADLINE).await;
    let mut gone = lent;
    gone.extend(found.into_iter().filter(|cookie| {
        cookie
            .domain()
            .is_some_and(|d| cookie_domain_matches(d, host))
    }));
    sign_in::forget(webview, gone).await;
}

fn window_error(error: tauri::Error) -> OAuthError {
    OAuthError::Window(error.to_string())
}

/// Shows `auth_url` on `profile`'s storage and its session, and answers with
/// the URL it was redirected to, query and all, once it reaches
/// `redirect_uri`. Closing the window, [`DEADLINE`] passing and a second
/// request while one is open each answer their [`OAuthError`].
pub async fn open_authorization_window<R: Runtime>(
    app: &AppHandle<R>,
    profile: &ProfileNet,
    auth_url: &str,
    redirect_uri: &str,
) -> Result<Url, OAuthError> {
    authorize_within(app, profile, auth_url, redirect_uri, DEADLINE).await
}

async fn authorize_within<R: Runtime>(
    app: &AppHandle<R>,
    profile: &ProfileNet,
    auth_url: &str,
    redirect_uri: &str,
    deadline: Duration,
) -> Result<Url, OAuthError> {
    let run = Run::new(auth_url, redirect_uri, deadline)?;
    let label = format!("oauth-{}", WINDOWS.fetch_add(1, Ordering::Relaxed));
    let windows = app.state::<OAuthWindows>().inner().clone();
    match windows.begin(&label, &profile.id, run.pending.clone()) {
        Ok(()) => {}
        Err(None) => return Err(OAuthError::SigningIn),
        Err(Some(open)) => {
            // As a second sign-in would: the person is shown the one they have.
            if let Some(window) = app.get_webview_window(&open) {
                let _ = window.set_focus();
            }
            return Err(OAuthError::Busy);
        }
    }
    let lease = Lease::new(windows, &run);
    let pending = run.pending.clone();
    run.drive(lease, &profile.jar, &|| {
        open(app, &label, &profile.id, pending.clone())
    })
    .await
}

fn open<R: Runtime>(
    app: &AppHandle<R>,
    label: &str,
    profile: &str,
    pending: Arc<Pending>,
) -> Result<Arc<dyn AuthWindow>, OAuthError> {
    let window = handlers::build(app, label, profile, pending).map_err(window_error)?;
    Ok(Arc::new(TauriWindow(window)))
}

/// What opens a run's window: Tauri's, or a test's.
type Open<'a> = &'a (dyn Fn() -> Result<Arc<dyn AuthWindow>, OAuthError> + Sync);

struct Run {
    page: Url,
    host: String,
    pending: Arc<Pending>,
    ended: oneshot::Receiver<Ended>,
    deadline: Duration,
    /// See [`TAKE_BACK_DEADLINE`].
    take_back: Duration,
}

impl Run {
    fn new(auth_url: &str, redirect_uri: &str, deadline: Duration) -> Result<Self, OAuthError> {
        let flow = Flow::new(auth_url, redirect_uri)?;
        let (page, host) = (flow.page().clone(), flow.host().to_string());
        let (done, ended) = oneshot::channel();
        let pending = Arc::new(Pending {
            flow: Mutex::new(flow),
            done: Mutex::new(Some(done)),
            destroying: AtomicBool::new(false),
        });
        Ok(Self {
            page,
            host,
            pending,
            ended,
            deadline,
            take_back: TAKE_BACK_DEADLINE,
        })
    }

    /// Lends the window its session, shows the page and waits for the
    /// ending; then `lease` takes the session back and destroys the window.
    async fn drive(mut self, mut lease: Lease, jar: &ProfileJar, open: Open<'_>) -> Ended {
        let window = open()?;
        let lent = lent(jar, &self.page, &self.host);
        lease.window = Some((window.clone(), lent.clone()));
        lend(window.clone(), lent.clone()).await;
        // A page that cannot load ends the run now, not at the deadline.
        if let Err(error) = window.navigate(self.page.clone()) {
            self.pending.end(error);
        }
        let ended = match tokio::time::timeout(self.deadline, &mut self.ended).await {
            Ok(ended) => ended,
            Err(_) => {
                self.pending.end(OAuthError::TimedOut);
                self.ended.await
            }
        };
        lease.release(window, lent).await;
        ended.unwrap_or(Err(OAuthError::Closed))
    }
}

/// A run's hold on the slot and, once it has one, its window and what the
/// window was lent. However the run stops, at its end, dropped with the
/// request that awaited it, or in a panic, the session is taken back and the
/// window destroyed before the slot is free: a slot never freed would answer
/// every later request Busy, with the session still lent.
struct Lease {
    windows: OAuthWindows,
    pending: Arc<Pending>,
    host: String,
    take_back: Duration,
    window: Option<(Arc<dyn AuthWindow>, Vec<RawCookie<'static>>)>,
}

impl Lease {
    fn new(windows: OAuthWindows, run: &Run) -> Self {
        Self {
            windows,
            pending: run.pending.clone(),
            host: run.host.clone(),
            take_back: run.take_back,
            window: None,
        }
    }

    /// Closes the window it holds. Dropped part way, the drop does it again.
    async fn release(mut self, window: Arc<dyn AuthWindow>, lent: Vec<RawCookie<'static>>) {
        close(window, lent, &self.pending, &self.host, self.take_back).await;
        self.window = None;
    }
}

impl Drop for Lease {
    fn drop(&mut self) {
        let windows = self.windows.clone();
        let Some((window, lent)) = self.window.take() else {
            windows.finish();
            return;
        };
        let (pending, host, cap) = (self.pending.clone(), self.host.clone(), self.take_back);
        tauri::async_runtime::spawn(async move {
            close(window, lent, &pending, &host, cap).await;
            windows.finish();
        });
    }
}

/// Takes the session back, for `cap` at most, then destroys the window
/// whether or not that was done.
async fn close(
    window: Arc<dyn AuthWindow>,
    lent: Vec<RawCookie<'static>>,
    pending: &Pending,
    host: &str,
    cap: Duration,
) {
    let taken = tokio::time::timeout(cap, take_back(window.clone(), lent, host)).await;
    if taken.is_err() {
        eprintln!("[oauth] the window did not give its cookies back in time; it is closed anyway");
    }
    pending.destroying.store(true, Ordering::SeqCst);
    window.destroy();
}

/// The extension host's `authWindow`, on the active profile: what Electron's
/// `openAuthorizationWindow` resolves with, the redirect's URL, or `null`
/// when the person closed the window. Anything else is a failure with its
/// reason, which `supervisor-auth.js` shows as it is.
pub async fn for_host<R: Runtime>(
    app: AppHandle<R>,
    auth_url: String,
    redirect_uri: String,
) -> Result<Value, String> {
    let net = app.state::<Arc<Network>>().active().await;
    let ended = open_authorization_window(&app, &net, &auth_url, &redirect_uri).await;
    match ended {
        Ok(url) => Ok(Value::String(url.into())),
        Err(OAuthError::Closed) => Ok(Value::Null),
        Err(error) => Err(error.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::chrome::Chrome;
    use crate::side_panel::tests::wait_for;
    use crate::sign_in::tests::FakeWebview;
    use serde_json::json;
    use tauri::async_runtime::{block_on, spawn, JoinHandle};
    use tauri::test::{mock_builder, mock_context, noop_assets, MockRuntime};
    use tauri::App;

    const SERVER: &str = "https://app.agentrq.example";
    const PAGE: &str = "https://app.agentrq.example/mcp/oauth2/authorize?client_id=c&state=s";
    const REDIRECT: &str = "agentrq://extensions/authorized";

    struct Rig {
        app: App<MockRuntime>,
        net: Arc<ProfileNet>,
        _profiles: crate::profiles::tests::Rig,
    }

    impl Rig {
        fn windows(&self) -> tauri::State<'_, OAuthWindows> {
            self.app.state::<OAuthWindows>()
        }

        /// Starts an authorisation, and waits until its window is built.
        fn start(&self, deadline: Duration) -> JoinHandle<Ended> {
            let (app, net) = (self.app.handle().clone(), self.net.clone());
            let run =
                spawn(async move { authorize_within(&app, &net, PAGE, REDIRECT, deadline).await });
            wait_for("the window is built", &|| {
                self.windows()
                    .current()
                    .is_some_and(|(label, _, _)| self.app.get_webview_window(&label).is_some())
            });
            run
        }

        fn pending(&self) -> Arc<Pending> {
            self.windows()
                .current()
                .expect("an authorisation under way")
                .2
        }

        fn wait_done(&self) {
            wait_for("the slot is free", &|| self.windows().current().is_none());
        }
    }

    fn rig() -> Rig {
        let profiles = crate::profiles::tests::rig_with(
            Some(json!({
                "activeProfileId": "default",
                "profiles": [
                    {"id": "default", "serverUrl": SERVER},
                    {"id": "work", "serverUrl": SERVER},
                ],
            })),
            None,
        );
        let app = mock_builder()
            .manage(profiles.network.clone())
            .manage(profiles.profiles.clone())
            .manage(Chrome::new(profiles.dir.path().to_path_buf()))
            .manage(profiles.profiles.auth_windows())
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let net = block_on(profiles.network.profile("default", SERVER));
        // Signed in, so the window is lent a session.
        assert!(net
            .jar
            .adopt(&cookie("at=a1; Path=/; HttpOnly"), &url(SERVER)));
        Rig {
            app,
            net,
            _profiles: profiles,
        }
    }

    fn url(text: &str) -> Url {
        Url::parse(text).unwrap()
    }

    fn cookie(text: &str) -> RawCookie<'static> {
        RawCookie::parse(text.to_string()).unwrap()
    }

    #[test]
    fn answers_with_the_redirect_cancelled_before_it_loads() {
        let rig = rig();
        let run = rig.start(DEADLINE);
        let pending = rig.pending();
        // The page, a provider's sign-in and back are the web; the app's own
        // links and other schemes never leave the window.
        assert!(pending.navigate(&url(PAGE)));
        assert!(pending.navigate(&url("https://github.com/login")));
        assert!(!pending.navigate(&url("agentrq://open?route=/x")));
        assert!(!pending.navigate(&url("agentrq://extensions/authorizedX?code=c")));
        let landed = url("agentrq://extensions/authorized?code=c&state=s");
        assert!(!pending.navigate(&landed));
        assert_eq!(block_on(run).unwrap(), Ok(landed));
        rig.wait_done();
        // What the window does while it closes goes nowhere.
        assert!(!pending.navigate(&url(PAGE)));
        assert!(!pending.end(OAuthError::Closed));
    }

    #[test]
    fn closing_the_window_is_declining() {
        let rig = rig();
        let run = rig.start(DEADLINE);
        assert!(rig.pending().close_requested(), "held for the take-back");
        assert_eq!(block_on(run).unwrap(), Err(OAuthError::Closed));
        rig.wait_done();
    }

    /// The run that `end` wakes may destroy the window before `end` returns:
    /// the close is still held, since `destroying` was read first. Holding
    /// `done` keeps `end` from returning until the run has flipped it.
    #[test]
    fn a_close_is_held_though_the_run_destroys_the_window_meanwhile() {
        let run = Run::new(PAGE, REDIRECT, DEADLINE).unwrap();
        let pending = run.pending.clone();
        let sending = lock(&pending.done);
        let close = std::thread::spawn({
            let pending = pending.clone();
            move || pending.close_requested()
        });
        wait_for("the flow ended", &|| !pending.navigate_frame(PAGE));
        pending.destroying.store(true, Ordering::SeqCst);
        drop(sending);
        assert!(close.join().unwrap(), "held for the take-back");
        assert_eq!(block_on(run.ended).unwrap(), Err(OAuthError::Closed));
    }

    #[test]
    fn gives_up_at_the_deadline() {
        let rig = rig();
        let ended = block_on(authorize_within(
            rig.app.handle(),
            &rig.net,
            PAGE,
            REDIRECT,
            Duration::from_millis(20),
        ));
        assert_eq!(ended, Err(OAuthError::TimedOut));
        assert!(rig.windows().current().is_none());
    }

    #[test]
    fn a_second_request_is_refused_while_one_is_open() {
        let rig = rig();
        let first = rig.start(DEADLINE);
        let second = block_on(open_authorization_window(
            rig.app.handle(),
            &rig.net,
            PAGE,
            REDIRECT,
        ));
        assert_eq!(second, Err(OAuthError::Busy));
        // The first is unaffected, and the slot is free once it ends.
        let landed = url("agentrq://extensions/authorized?code=c");
        assert!(!rig.pending().navigate(&landed));
        assert_eq!(block_on(first).unwrap(), Ok(landed));
        rig.wait_done();
        let third = rig.start(DEADLINE);
        rig.pending().end(OAuthError::Closed);
        assert_eq!(block_on(third).unwrap(), Err(OAuthError::Closed));
    }

    #[test]
    fn opens_only_a_web_page() {
        let rig = rig();
        for (page, redirect) in [("javascript:alert(1)", REDIRECT), (PAGE, "not a url")] {
            let ended = block_on(authorize_within(
                rig.app.handle(),
                &rig.net,
                page,
                redirect,
                DEADLINE,
            ));
            assert_eq!(ended, Err(OAuthError::BadUrl));
        }
        assert!(rig.windows().current().is_none());
    }

    #[test]
    fn a_window_that_cannot_be_built_says_why() {
        let error = window_error(tauri::Error::WebviewLabelAlreadyExists("oauth-1".into()));
        assert!(matches!(error, OAuthError::Window(why) if why.contains("oauth-1")));
    }

    #[test]
    fn a_subframe_loads_what_the_page_may_and_never_ends_the_flow() {
        let run = Run::new(PAGE, REDIRECT, DEADLINE).unwrap();
        let pending = &run.pending;
        assert!(pending.navigate_frame("https://github.com/login"));
        for refused in [
            "agentrq://extensions/authorized?code=c&state=s",
            "agentrq://open?route=/x",
            "data:text/html,x",
            "not a url",
        ] {
            assert!(!pending.navigate_frame(refused), "{refused}");
        }
        // Still waiting for the page's own redirect.
        assert!(pending.end(OAuthError::Closed));
    }

    /// A window whose store is a [`FakeWebview`], and which says what it was
    /// asked to do, in order.
    #[derive(Default)]
    struct FakeWindow {
        store: FakeWebview,
        did: Mutex<Vec<String>>,
        fails: Option<OAuthError>,
        /// When set, a read of its cookies waits until the test sends.
        held: Option<Mutex<std::sync::mpsc::Receiver<()>>>,
    }

    impl FakeWindow {
        fn did(&self, what: String) {
            self.did.lock().unwrap().push(what);
        }

        /// What it did, a step done twice in a row counted once.
        fn steps(&self) -> Vec<String> {
            let mut steps = self.did.lock().unwrap().clone();
            steps.dedup();
            steps
        }

        fn has_done(&self, step: &str) -> bool {
            self.steps().iter().any(|done| done == step)
        }
    }

    impl CookieWebview for FakeWindow {
        fn cookies(&self) -> Vec<RawCookie<'static>> {
            self.did("read".into());
            if let Some(held) = &self.held {
                let _ = held.lock().unwrap().recv();
            }
            self.store.cookies()
        }
        fn delete_cookie(&self, cookie: RawCookie<'static>) {
            self.did(format!("delete {}", cookie.name()));
            self.store.delete_cookie(cookie);
        }
    }

    impl SessionWebview for FakeWindow {
        fn set_cookie(&self, cookie: RawCookie<'static>) {
            self.did(format!("lend {}", cookie.name()));
            self.store.set_cookie(cookie);
        }
    }

    impl AuthWindow for FakeWindow {
        fn navigate(&self, url: Url) -> Result<(), OAuthError> {
            self.did(format!("navigate {url}"));
            self.fails.clone().map_or(Ok(()), Err)
        }
        fn destroy(&self) {
            self.did("destroy".into());
        }
    }

    /// A run on `window`, or on none that will open, started as
    /// `authorize_within` starts one.
    struct Driven {
        windows: OAuthWindows,
        pending: Arc<Pending>,
        run: JoinHandle<Ended>,
    }

    fn drive(window: Option<Arc<FakeWindow>>) -> Driven {
        drive_capped(window, TAKE_BACK_DEADLINE)
    }

    /// [`drive`], taking the session back for `take_back` at most.
    fn drive_capped(window: Option<Arc<FakeWindow>>, take_back: Duration) -> Driven {
        let jar = ProfileJar::open("unused".into(), None);
        assert!(jar.adopt(
            &cookie("at=a1; Path=/; HttpOnly; Secure; SameSite=Lax"),
            &url(SERVER)
        ));
        let mut run = Run::new(PAGE, REDIRECT, DEADLINE).unwrap();
        run.take_back = take_back;
        let windows = OAuthWindows::default();
        windows
            .begin("oauth-fake", "default", run.pending.clone())
            .unwrap();
        let (lease, pending) = (Lease::new(windows.clone(), &run), run.pending.clone());
        let run = spawn(async move {
            let open = || -> Result<Arc<dyn AuthWindow>, OAuthError> {
                let window = window.clone();
                window
                    .map(|window| window as Arc<dyn AuthWindow>)
                    .ok_or_else(|| OAuthError::Window("no display".into()))
            };
            run.drive(lease, &jar, &open).await
        });
        Driven {
            windows,
            pending,
            run,
        }
    }

    fn wait_shown(window: &FakeWindow) {
        wait_for("the page is shown", &|| {
            window.has_done(&format!("navigate {PAGE}"))
        });
    }

    fn wait_free(windows: &OAuthWindows) {
        wait_for("the slot is free", &|| windows.current().is_none());
    }

    #[test]
    fn lends_shows_takes_back_and_destroys_in_that_order() {
        let window = Arc::new(FakeWindow::default());
        let driven = drive(Some(window.clone()));
        wait_shown(&window);
        let landed = url("agentrq://extensions/authorized?code=c");
        assert!(!driven.pending.navigate(&landed));
        assert_eq!(block_on(driven.run).unwrap(), Ok(landed));
        assert_eq!(
            window.steps(),
            [
                "lend at".to_string(),
                format!("navigate {PAGE}"),
                "read".into(),
                "delete at".into(),
                "destroy".into(),
            ]
        );
        assert!(window.store.names().is_empty());
        wait_free(&driven.windows);
    }

    #[test]
    fn a_page_that_cannot_load_ends_the_run_at_once() {
        let window = Arc::new(FakeWindow {
            fails: Some(OAuthError::Window("no network".into())),
            ..FakeWindow::default()
        });
        let driven = drive(Some(window.clone()));
        assert_eq!(
            block_on(driven.run).unwrap(),
            Err(OAuthError::Window("no network".into()))
        );
        assert_eq!(window.steps().last().map(String::as_str), Some("destroy"));
        assert!(window.store.names().is_empty());
        wait_free(&driven.windows);
    }

    #[test]
    fn a_window_that_does_not_open_frees_the_slot() {
        let driven = drive(None);
        assert_eq!(
            block_on(driven.run).unwrap(),
            Err(OAuthError::Window("no display".into()))
        );
        wait_free(&driven.windows);
    }

    #[test]
    fn a_run_dropped_part_way_still_takes_the_session_back() {
        let window = Arc::new(FakeWindow::default());
        let driven = drive(Some(window.clone()));
        wait_shown(&window);
        driven.run.abort();
        wait_free(&driven.windows);
        assert_eq!(window.steps().last().map(String::as_str), Some("destroy"));
        assert!(window.store.names().is_empty(), "the session is back");
    }

    #[test]
    fn a_close_while_the_session_is_taken_back_waits_for_it() {
        let (go_on, held) = std::sync::mpsc::channel();
        let window = Arc::new(FakeWindow {
            held: Some(Mutex::new(held)),
            ..FakeWindow::default()
        });
        let driven = drive(Some(window.clone()));
        wait_shown(&window);
        assert!(!driven.pending.navigate(&url(REDIRECT)));
        wait_for("the take-back has begun", &|| window.has_done("read"));
        assert!(driven.pending.close_requested(), "the close waits");
        go_on.send(()).unwrap();
        assert_eq!(block_on(driven.run).unwrap(), Ok(url(REDIRECT)));
        assert!(window.store.names().is_empty());
        // Destroyed: a close now would go.
        assert!(!driven.pending.close_requested());
        wait_free(&driven.windows);
    }

    #[test]
    fn a_take_back_that_never_ends_is_given_up_and_the_window_destroyed() {
        let (go_on, held) = std::sync::mpsc::channel();
        let window = Arc::new(FakeWindow {
            held: Some(Mutex::new(held)),
            ..FakeWindow::default()
        });
        let driven = drive_capped(Some(window.clone()), Duration::from_millis(50));
        wait_shown(&window);
        assert!(driven.pending.close_requested(), "held for the take-back");
        // The webview never answers the read, yet the run ends and frees the slot.
        assert_eq!(block_on(driven.run).unwrap(), Err(OAuthError::Closed));
        assert_eq!(window.steps().last().map(String::as_str), Some("destroy"));
        assert!(!driven.pending.close_requested(), "a close now goes");
        wait_free(&driven.windows);
        drop(go_on);
        assert_eq!(TAKE_BACK_DEADLINE, 2 * COOKIE_DEADLINE);
    }

    #[test]
    fn ending_a_profiles_window_waits_until_its_session_is_back_and_it_is_gone() {
        let rig = rig();
        let run = rig.start(DEADLINE);
        // Another profile's: nothing to end.
        block_on(rig.windows().close_for("work"));
        assert!(rig.windows().current().is_some());
        // A sign-in on this one ends it, and opens once it has gone.
        block_on(rig.windows().close_for("default"));
        assert!(rig.windows().current().is_none());
        assert_eq!(block_on(run).unwrap(), Err(OAuthError::Closed));
        // Nothing open: nothing to wait for.
        block_on(rig.windows().close_for("default"));
    }

    #[test]
    fn a_sign_in_opens_once_the_window_on_its_store_is_gone_and_holds_off_another() {
        let rig = rig();
        let run = rig.start(DEADLINE);
        let (opened, sign_ins) = std::sync::mpsc::channel();
        let windows = rig.windows().inner().clone();
        let open: SignInOpener<MockRuntime> = Box::new(move |_, start, held| {
            // The window on this store has given its session back and gone.
            opened.send((start, windows.current(), held)).unwrap();
        });
        open_sign_in(
            rig.app.handle(),
            "https://app.agentrq.example/login".into(),
            open,
        );
        let (start, open_then, held) = sign_ins
            .recv_timeout(Duration::from_secs(10))
            .expect("the sign-in opens");
        assert_eq!(start, "https://app.agentrq.example/login");
        assert!(open_then.is_none());
        assert_eq!(block_on(run).unwrap(), Err(OAuthError::Closed));
        // While it is open, no authorisation shares its store; another
        // profile's still opens.
        let refused = block_on(authorize_within(
            rig.app.handle(),
            &rig.net,
            PAGE,
            REDIRECT,
            DEADLINE,
        ));
        assert_eq!(refused, Err(OAuthError::SigningIn));
        assert!(rig.windows().current().is_none());
        let second = block_on(rig.windows().sign_in("default"));
        drop(held);
        let still = block_on(open_authorization_window(
            rig.app.handle(),
            &rig.net,
            PAGE,
            REDIRECT,
        ));
        assert_eq!(still, Err(OAuthError::SigningIn), "one sign-in is open yet");
        let (app, network) = (rig.app.handle().clone(), rig._profiles.network.clone());
        let work = spawn(async move {
            let work = network.profile("work", SERVER).await;
            authorize_within(&app, &work, PAGE, REDIRECT, DEADLINE).await
        });
        wait_for("the window is built", &|| rig.windows().current().is_some());
        rig.pending().end(OAuthError::Closed);
        assert_eq!(block_on(work).unwrap(), Err(OAuthError::Closed));
        rig.wait_done();
        // Once the sign-in windows are gone, it opens again.
        drop(second);
        let again = rig.start(DEADLINE);
        rig.pending().end(OAuthError::Closed);
        assert_eq!(block_on(again).unwrap(), Err(OAuthError::Closed));
    }

    /// An opener that says which profile its window would show.
    fn says_profile(opened: std::sync::mpsc::Sender<String>) -> SignInOpener<MockRuntime> {
        Box::new(move |_, _, held| opened.send(held.profile().to_string()).unwrap())
    }

    #[test]
    fn a_sign_in_is_dropped_when_another_profile_is_opened_during_the_take_back() {
        let rig = rig();
        let windows = rig.windows().inner().clone();
        // A window on this store whose take-back lasts until the test says.
        let run = Run::new(PAGE, REDIRECT, DEADLINE).unwrap();
        windows
            .begin("oauth-x", "default", run.pending.clone())
            .unwrap();
        let (opened, sign_ins) = std::sync::mpsc::channel();
        open_sign_in(rig.app.handle(), SERVER.into(), says_profile(opened));
        // Held, on the profile shown when it was asked for.
        wait_for("the sign-in holds its store", &|| {
            lock(&windows.signing_in).contains_key("default")
        });
        block_on(rig._profiles.profiles.switch("work")).unwrap();
        windows.finish();
        wait_for("the hold is let go", &|| {
            lock(&windows.signing_in).is_empty()
        });
        assert!(sign_ins.try_recv().is_err(), "no window opens");

        // With no switch, the window is handed the profile it holds.
        let (opened, sign_ins) = std::sync::mpsc::channel();
        open_sign_in(rig.app.handle(), SERVER.into(), says_profile(opened));
        let profile = sign_ins.recv_timeout(Duration::from_secs(10)).unwrap();
        assert_eq!(profile, "work");
    }

    #[test]
    fn a_window_keeps_its_hold_until_it_is_destroyed() {
        let windows = OAuthWindows::default();
        let held = HeldSignIn::new(block_on(windows.sign_in("default")));
        held.on_window_event(&WindowEvent::Focused(true));
        assert!(lock(&windows.signing_in).contains_key("default"));
        held.on_window_event(&WindowEvent::Destroyed);
        assert!(lock(&windows.signing_in).is_empty());
        // Gone once: the next event finds nothing to let go.
        held.on_window_event(&WindowEvent::Destroyed);
    }

    #[test]
    fn removing_a_profile_ends_its_window_before_its_store_goes() {
        let rig = rig();
        let (app, profiles) = (rig.app.handle().clone(), rig._profiles.network.clone());
        let run = spawn(async move {
            let work = profiles.profile("work", SERVER).await;
            authorize_within(&app, &work, PAGE, REDIRECT, DEADLINE).await
        });
        wait_for("the window is built", &|| {
            rig.windows()
                .current()
                .is_some_and(|(_, on, _)| on == "work")
        });
        block_on(rig._profiles.profiles.remove("work")).unwrap();
        assert!(
            rig.windows().current().is_none(),
            "gone before remove answers"
        );
        assert_eq!(block_on(run).unwrap(), Err(OAuthError::Closed));
    }

    #[test]
    fn answers_the_extension_host_as_electron_does() {
        let rig = rig();
        let ask = |rig: &Rig| {
            let app = rig.app.handle().clone();
            spawn(for_host(app, PAGE.into(), REDIRECT.into()))
        };
        let wait_open = |rig: &Rig| {
            wait_for("the window is built", &|| rig.windows().current().is_some());
        };
        // The redirect's URL, whole.
        let asked = ask(&rig);
        wait_open(&rig);
        let landed = format!("{REDIRECT}?code=c&state=s");
        assert!(!rig.pending().navigate(&url(&landed)));
        assert_eq!(block_on(asked).unwrap(), Ok(json!(landed)));
        rig.wait_done();
        // Closed: `null`, as Electron's window resolves.
        let asked = ask(&rig);
        wait_open(&rig);
        rig.pending().close_requested();
        assert_eq!(block_on(asked).unwrap(), Ok(Value::Null));
        rig.wait_done();
        // Anything else is why.
        let bad = block_on(for_host(
            rig.app.handle().clone(),
            "x".into(),
            REDIRECT.into(),
        ));
        assert_eq!(bad, Err(OAuthError::BadUrl.to_string()));
    }

    #[test]
    fn the_host_authorises_through_the_window() {
        use crate::ext_host::tests::script_launch;
        use crate::ext_host::{ExtHost, Services};
        use crate::extensions::ShellServices;
        use crate::network::tests::NoKeychain;
        let rig = rig();
        // `authorize` asks the shell for its window, and answers with that.
        let script = r#"
            const out = (m) => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...m }) + '\n');
            let asked = null;
            require('readline').createInterface({ input: process.stdin }).on('line', (line) => {
                const m = JSON.parse(line);
                if (m.method === 'authorize') {
                    asked = m.id;
                    out({ id: 1000, method: 'authWindow', params: m.params });
                } else if (m.id === 1000) {
                    out({ id: asked, result: m.error ? { error: m.error.message } : { redirect: m.result } });
                }
            });
        "#;
        let app = rig.app.handle().clone();
        let services: Arc<dyn Services> = Arc::new(ShellServices::new(
            rig._profiles.network.clone(),
            Arc::new(NoKeychain { delay_ms: 0 }),
            crate::extensions::tests::untold(),
            Box::new(move |url, redirect| Box::pin(for_host(app.clone(), url, redirect))),
        ));
        let dir = tempfile::tempdir().unwrap();
        let host = ExtHost::new(script_launch(script), dir.path().into(), services);
        let caller = host.clone();
        let params = json!({ "url": PAGE, "redirectUri": REDIRECT });
        let asked = spawn(async move { caller.call("authorize", params).await });
        wait_for("the window is built", &|| rig.windows().current().is_some());
        let landed = format!("{REDIRECT}?code=c&state=s");
        assert!(!rig.pending().navigate(&url(&landed)));
        assert_eq!(block_on(asked).unwrap(), Ok(json!({ "redirect": landed })));
        rig.wait_done();
        block_on(async { host.stop() });
    }

    impl SessionWebview for FakeWebview {
        fn set_cookie(&self, cookie: RawCookie<'static>) {
            let domain = cookie.domain().unwrap_or_default().to_string();
            self.store.lock().unwrap().push((domain, cookie));
        }
    }

    #[test]
    fn lends_the_pages_cookies_for_its_host_and_this_session_only() {
        let dir = tempfile::tempdir().unwrap();
        let jar = ProfileJar::open(dir.path().join("jar"), None);
        let server = url(SERVER);
        for text in [
            "at=a1; Path=/; HttpOnly; Secure; Max-Age=3600; SameSite=Lax",
            "pref=p1; Path=/; Secure; SameSite=Strict",
            "rt=r1; Path=/api/v1/auth/refresh; HttpOnly; Secure; Max-Age=3600",
        ] {
            assert!(jar.adopt(&cookie(text), &server));
        }
        let page = url(PAGE);
        let mut lent = lent(&jar, &page, "app.agentrq.example");
        lent.sort_by(|a, b| a.name().cmp(b.name()));
        assert_eq!(lent.len(), 2, "the refresh cookie is not for this page");
        let at = &lent[0];
        assert_eq!((at.name(), at.value()), ("at", "a1"));
        assert_eq!(at.domain(), Some("app.agentrq.example"));
        assert_eq!(at.path(), Some("/"));
        assert_eq!((at.http_only(), at.secure()), (Some(true), Some(true)));
        assert_eq!(at.expires(), None);
        // As the server set it.
        assert_eq!(at.same_site(), Some(SameSite::Lax));
        assert_eq!(lent[1].same_site(), Some(SameSite::Strict));
        // A local server without TLS: no secure cookie is sent, or lent, and
        // one set with no SameSite is lent as Lax, never as None.
        let local = url("http://127.0.0.1:3000/");
        assert!(jar.adopt(&cookie("at=a2; Path=/; HttpOnly"), &local));
        let plain = super::lent(&jar, &local, "127.0.0.1");
        assert_eq!((plain[0].value(), plain[0].secure()), ("a2", Some(false)));
        assert_eq!(plain[0].same_site(), Some(SameSite::Lax));
    }

    #[test]
    fn takes_back_what_was_lent_and_any_session_for_the_host() {
        let host = "app.agentrq.example";
        let webview = Arc::new(FakeWebview::with(&[
            ("gh=1; Path=/", "github.com"),
            ("rt=r2; Path=/api/v1/auth/refresh", host),
            ("other=1; Path=/", "agentrq.example.evil"),
        ]));
        let mut at = RawCookie::new("at", "a1");
        at.set_domain(host);
        at.set_path("/");
        block_on(async {
            lend(webview.clone(), vec![at.clone()]).await;
            assert_eq!(webview.names(), ["gh", "rt", "other", "at"]);
            take_back(webview.clone(), vec![at], host).await;
        });
        assert_eq!(webview.names(), ["gh", "other"]);
    }
}
