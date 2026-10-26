# Tauri desktop app (`desktop-tauri/`, preview)

> Read before changing anything under `desktop-tauri/`. It renders desktop's
> renderer unchanged, so [the desktop note](desktop.md) applies here too. How to
> build, test and verify it is in [`desktop-tauri/README.md`](../../desktop-tauri/README.md);
> what is checked on which OS is in [the parity checklist](desktop-tauri-parity.md).

## Where code goes, and the gates

- **Pure logic goes in `core/`**, which has no Tauri dependency and a 100% line
  gate. `src-tauri/src/lib.rs` is wiring only and is outside the gate.
- **`chrome.rs`, `app_menu.rs`, `side_panel/`'s platform files and `handlers.rs`, and `oauth_window/handlers.rs`
  are wiring and outside the coverage gate too**; a rule goes in `core/` or a
  gated file with its test. `side_panel.rs` is gated: llvm-cov counts a closure
  per generic instance, so keep closures out of its commands. An async command whose
  arguments cannot fail (`AppHandle`, `Webview`) leaves its attribute line uncovered:
  take a `State`, which fails when unmanaged, and test that.
- **`make stop` must leave the Tauri dev app alone**: it skips any vite under
  `desktop-tauri/`, and `tauri dev` exits with its Vite (port 5175).
- **`AGENTRQ_E2E_INIT_SCRIPT` is CI's**: a script the main webview runs first, which seeds
  the theme for the screenshot job (`test/e2e/`). Only a debug build or `--features e2e`
  reads it; a release must not run an environment's script in the webview.
- **The verify scripts see one profile's window only.** WebKitWebDriver automates the web
  context it launched with, so after `profiles.add()` the session has no window; a save
  or switch rebuilds `main` under the *same* handle, so `replaceWindow` tells old from new
  by a marker. A check that follows a rebuild must wait for the new page's own state
  (`/login`), or its first fetch goes out before the network has moved.
- **WebDriver cannot reach a side panel guest**, so a verify script reads one from a
  stand-in site's log (`scripts/lib/site.mjs`): its requests, and a beat its page sends
  every 100 ms. A closed guest is a beat that never comes again after a page known to
  have started later. Take the log's position *before* the action that makes the page
  load, or its hello can land first and the wait never sees it.
- **Flaky Tauri Rust tests come from runner stalls (>1 s), not CPU load.** Reproduce
  by SIGSTOP/SIGCONT-ing the test process for 160–300 ms at random; stress-ng alone
  never fails them. Use `side_panel::tests::wait_for(what, cond)` (10 s deadline, asserts)
  instead of fixed sleeps; a wait must assert. For microsecond-long spawned work the
  rig can't open a gap, so prove a fix with an injected delay.
- **A log line that only runs on slow runs flakes the 100% line-coverage gate.**

## The bridge and its commands

- **A bridge command is named after its Electron channel** (`agentrq:side-panel:permission-answer`
  is `side_panel_permission_answer`). `commands.rs` holds one per channel, sometimes
  with one in a module of its own (`extensions.rs`), which the parity test then reads too.
- **`bridge/index.js` mirrors `desktop/src/preload/index.js` member for member**, and
  `test/parity.test.js` loads both. Change the preload and the bridge follows, or CI
  fails. A call's arguments cross as one object keyed by the preload's parameter names.
- **A new command is five places:** `commands.rs`, its `generate_handler!`, `COMMANDS` in
  `build.rs`, `capabilities/main.json` (`allow-<name-with-dashes>`) and the bridge. Naming
  the commands in `build.rs` is what makes Tauri deny any other, so a command missing
  from the capability fails at runtime, not at build. The parity test compares all five.
- **A command's arguments are required unless the page may leave them out.** An `Option`
  turns a key the bridge misspelled (`fileUrl` for `rawUrl`) into `None`, silently.
  The parity test checks every implemented command's parameter names against the bridge.
- **Scope the capability with `webviews`, never `windows`.** A window grant also covers
  every child webview in it, so side-panel guests and `agentrq-ext:` pages would
  inherit all our commands. Guests get no capability; a test reads every capability file.
- **A guest still has Tauri's IPC object** (`__TAURI_INTERNALS__`, `isTauri`), and the
  ACL is all that refuses its calls. Nothing can take the object out. Tauri prepends it
  to every webview with no per-webview switch, and defines it non-configurable. On
  Windows, wry discards the WebView2 script ids, so it cannot be removed natively either.
- **`window.agentrq` is deep-frozen and non-writable** (`versions` stays mutable until the
  Tauri version resolves), as Electron's contextBridge leaves it. Page code must not be
  able to replace a member.

## The `app` scheme, the proxy and cookies

- **Every server call goes through the active profile's client** (`network.rs`):
  its jar is the session. A shared or default client signs two profiles in as one.
- **The proxy buffers; an `EventSource` goes over IPC.** The scheme answers only
  once it has the whole body, so an endless stream through it never answers.
- **Flatten request bodies in the bridge's `fetch` wrapper.** A Blob or File body
  sent to the `app` scheme segfaults WebKitGTK and arrives empty on WKWebView.
  Same-origin Blob, FormData and stream bodies go as an ArrayBuffer.
- **Same-origin in the bridge compares scheme and host, never `origin`**: a
  custom scheme's origin is opaque (`"null"`).
- **Tauri's asset resolver answers `index.html` for any missing path.** Ask the
  embedded keys whether a file exists, or `plan_static_request`'s 404 never fires.
- **Every proxied or cached response carries `nosniff` and `sandbox` (core's
  `PROXIED_RESPONSE_GUARDS`).** It is served on the app's own origin, so a framed
  HTML attachment would otherwise run as the app and reach `__TAURI_INTERNALS__`.
- **A Chromium-style `sandbox` CSP blocks the PDF viewer; only exactly `application/pdf`
  is exempt, with nosniff kept.**
- **The app registers its own `tauri` scheme, a 403**, so no guest reaches the
  built-in asset server (`tauri://localhost`, `http://tauri.localhost`).
- **A cookie jar with no vault never writes its file.** A slow or refused
  keychain leaves the session in memory; a jar resealed with a stand-in key
  would sign the person out for good.
- **With no vault, only the active profile's network is kept** (`Network::open`).
  Any other is opened afresh on each use: its jar can never read its file, so a
  kept one would show that profile signed out for the rest of the session.
- **The connection probe uses a client with no cookies that follows
  redirects**, never a profile's client. A probe must not sign anyone in.

## Sign-in and webview cookies

- **A sign-in window moves every cookie for the server's host into the jar**,
  not only `at`. The refresh cookie `rt` is what keeps the session past a day.
  The sign-in window gets no capability.
- **Read `at` with `cookies()` and our own domain-match, never `cookies_for_url`.**
  On macOS it matches the domain exactly, so it misses an IP host and the
  production `Domain=.<domain>` cookie.
- **Delete a webview cookie as both `host` and `.host`** (`sign_in::deletion_forms`).
  `cookie`'s `domain()` strips the leading dot and the stores match exactly, so
  passing back what `cookies()` gave deletes nothing.
- **Windows' `delete_cookie` is asynchronous.** Check the deletion after the next
  navigation, never straight after the call.
- **The supervisor's authorisation window is lent the jar's cookies for its page**
  (SameSite kept), and its `Lease` takes them back before the window is destroyed,
  even when the run is dropped; every close waits for that, for 20 s at most, then
  destroys it anyway: a stuck take-back would leave a window nothing can close. The
  redirect is matched exactly and cancelled; on Windows wry never asks about
  subframes, so `handlers.rs` guards them and external launches. WebKit asks about
  a subframe as the page, so there `supervisor-auth.js`'s state check is the guard.
- **A sign-in, or removing a profile, first ends that profile's authorisation
  window and waits for its take-back** (`OAuthWindows::close_for`). They share the
  store: a sign-in would move the lent `at` into the jar, and a removal would
  delete a store still in use. While a sign-in window is open, an authorisation on
  its store is refused (`OAuthWindows::sign_in`): its take-back would delete the
  session the sign-in set. The window shows the held profile's store, never the
  active one read again; a profile switch during the take-back drops the sign-in.

## Profiles and the main window

- **Only the webview labelled `main` gets the proxy and the capability**, so a
  profile switch cannot build the new window beside the old one. Destroy `main`
  and build the new one in the run loop's `Destroyed` event: the label is free
  by then and the window count never reaches zero, so Linux and Windows don't quit.
  Only `chrome::open_main_window` builds it, with one builder: Tauri keeps just the
  last `on_navigation` and `on_page_load`.
- **The network and a removed profile's storage change only once the old `main`
  is `Destroyed`** (`Profiles::main_window_closed`). Sooner, the old window's last
  requests go out on the new profile's session, and WebKit refuses to delete a
  store still in use.
- **A profile's webview storage lives under `profiles/<id>/`** (or its macOS
  data store), and start-up sweeps any profile folder or store that no profile owns.
- **Once a window has a child webview, `get_webview_window` finds nothing**
  (`get_webview_window("main")` returns `None`), and a `WebviewWindow` command
  argument fails: Tauri sees a webview window only while every webview shares the
  window's label. Use `chrome::main_webview` (or `get_webview("main")` plus
  `.window()`) and a `Webview` argument.
- **Never write localStorage and quit in the same tick.** One such write was lost
  on macOS 14.

## The side panel's guests

- **On Linux, Tauri stacks child webviews in a GtkBox**, so the side panel lands
  below the page. Put the main webview in a GtkOverlay and place the child with
  `get-child-position`.
- **Every webview but `main` gets 403 from the `app` scheme**, and only a
  `panel-guest-*` gets anything from `agentrq-ext`: schemes are app-wide. A guest
  has no capability, so its bridge is a `POST` to `/.agentrq/panel-send` on
  `agentrq-ext`, routed by the guest's own URL. It must carry the guest's token,
  which Rust pushes to the top frame on each load: WebKit sends no `Origin` on a
  same-origin custom-scheme request, so a missing `Origin` proves nothing.
- **Every guest is destroyed when `main` starts a load** (reload, crash, sign-out,
  profile switch); the new page knows none of them.
- **Before macOS 14 a guest is `incognito`**: wry ignores `data_store_identifier`
  there and the guest would share the default store.
- **WebKit makes no new window of a Cmd/Ctrl-click**, unlike Chromium, and wry
  reports no keys: the guest reads them natively in `on_navigation`, and only for
  a user's link click (a GLib emission hook on `decide-policy`, since wry's handler
  stops the signal), so a script's redirect never opens the browser.
- **The page keeps its `activeElement` while a guest has the keyboard**, so the
  guest reports its focus-in and the page blurs. Without it the address bar never
  follows the guest.
- **A guest's permission request waits for the window, never for wry.** With no
  handler wry *grants* a WKWebView the camera and microphone, and Tauri's own
  handler must answer at once; so each platform holds the request (a WebKitGTK
  request kept, a WebView2 deferral with `SavesInProfile(false)`, a copied
  WKWebView decision block) and core's broker answers it later, on the main
  thread. The broker reads its file: never call it on the main thread, and only
  `main` may answer, list or remove (`panel_permissions.rs`).
- **A WebKitGTK request is keyed by the guest's *committed* URI, never `uri()`**,
  which is already the next site's once a load starts while the old page can still
  ask: it would get that site's stored Allow. WebKitGTK names no frame, so a frame
  asks as its page (Permissions Policy limits that to delegated frames), and
  storage access, always a third party's, is refused. So is a screen, which
  Electron's guest cannot share.
- **An `Answer` dropped unanswered is a Block**, and a request is decided only
  for the guest's own profile and the document that made it. WebView2 raises one
  `getUserMedia` as two requests: halves within 250 ms are one question.
- **What a platform cannot ask about:** WKWebView has no public hook for
  geolocation, notifications or screen capture (none reaches the broker), and no
  media request at all before macOS 12; WebKitGTK before 2.42 says a screen request
  is neither audio nor video, which is how it is told apart. WebView2 autoplay is
  granted, as Electron's guest plays sound without asking.
- **The find key is caught natively, before the page:** a GTK `key-press-event` on
  the guest, `AcceleratorKeyPressed` with `Handled`, and an `NSEvent` local monitor
  scoped to guest views. A page cannot reach the guest bridge (no token, a
  cross-origin custom scheme, and its own CSP), so the init script cannot send it.
- **WebKit counts matches but never says which is active**, so core's
  `FindSession` keeps the ordinal; WKWebView does not even count, so a script does,
  in a content world of its own so the page never reads the text.

## Window, menu and keys

- **The window starts hidden and is shown on the page's first load** (5 s
  fallback), so a failed load still appears. Showing earlier is the white flash.
- **A webview process crash reloads the window** (WebKitGTK, WebView2), at most 3
  times a minute. WKWebView reports none through wry, so macOS cannot.
- **Drag regions are `data-tauri-drag-region="deep"`, and `="false"` on every
  `.app-no-drag`.** A bare attribute drags from that element only, not from the
  title text inside it, and a no-drag card of plain divs would drag without `"false"`.
- **Never use `tauri-plugin-window-state`.** Its clamping differs from
  `core::window_state`. The save is read when the debounce fires, never per
  resize event: a platform can report the maximized size before the state.
- **Zoom goes through `core::window_view` and `set_zoom`, never a native zoom
  shortcut.** Tauri cannot read a webview's zoom back, so `ZoomTable` is the only
  record; a native zoom would leave it, and the title bar's menu, wrong.
- **On macOS `ExitRequested { code: None }` must `prevent_exit()`**, or closing the
  window quits and the Dock's reopen never runs. Cmd+Q and Windows' Exit raise only
  `Exit`, so the state is saved there too.
- **Platform and build checks are `#[cfg]` consts, not `cfg!()`** (`PLATFORM`,
  `DEBUG_BUILD` in `chrome.rs` and `app_menu.rs`), so the code for another OS is
  never compiled in.
- **Off macOS the menu has no native accelerators.** Linux muda/GTK runs a window's
  accelerators before the page, so Ctrl+W closed the app under a terminal that
  wanted it, and Ctrl+R, Z, Q, M, 0 and - went the same way. The label keeps a
  hint, and the bridge runs the key instead:
  - from a bubbling `keydown` that is not `defaultPrevented`, checked a task later
    so a page listener on `window` can cancel it, against the table in
    `core::menu_keys` (Electron's rule);
  - the table leaves out undo and redo, which the webview does itself, and says
    which keys repeat (zoom only); `menu_run` takes only an id that has a key here
    and now;
  - a key's physical place is used only for a letter of a non-Latin script, never
    for `ß`, `¡`, `´` or a dead key;
  - WebView2's own accelerator keys are switched off, or Ctrl+R would reload
    twice, in a side panel guest too. Windows is otherwise unmeasured;
  - a guest's keys never reach that listener: on Linux the GtkOverlay takes the
    keys WebKit sends up unhandled; on Windows `AcceleratorKeyPressed` only notes
    the key and the guest's init script posts it if the page left it.

## Notifications, badge and tray

- **The unread count is the Notifier's (`notify.rs`); `ShellState` only shows it**,
  and a notification's click goes through `ShellState::navigate`, which holds the
  route through a reload.
- **Never call the `Desk` with a Notifier lock or `busy` held**: a tray update
  waits on the main thread, whose focus handler takes the count's lock. So counts
  and mute lists arrive in any order: each is numbered under its lock, and an
  older one is dropped.
- **Call `shell::page_leaving` before `main` is replaced or sent elsewhere**; its
  `Started` comes too late, and a click in between is lost.
- **Register the notification plugin as `notify::ShellOnly`, never bare.** Its
  init script replaces `window.Notification` in every webview, a guest's too,
  with one that calls commands no capability grants. A site could then never
  ask to notify.
- **A site's notification:** WebKitGTK's `show-notification` goes through the
  plugin while the site's Allow stands. An emission hook seeds each new web
  process with the answers. WebKitGTK refuses `requestPermission()` without a
  person's click, which Chromium does not. WebView2 shows its own (runtime 128+).
  WKWebView cannot, so `panel.js` tells a site `denied`.

## Extensions

- **Extensions run in a Node process, `agentrq-ext-host`**, which imports
  `desktop/src/main/extensions/*` unchanged; never fork them here. `tauri-build`
  refuses to build until `dist/ext-host/` exists: run `npm run build:ext-host`.
- **Node is never bundled; only `extensions_download_node` downloads it**, on the
  person's click, and every other command answers `needsNode`. The pins live only
  in `core::ext_node`; take a bump's hashes and sizes from the real archives.
- **The binary is checked against its pin, never a file beside it**, which whoever
  can write the binary can rewrite. The cache on size and mtime is a same-user gap,
  moot since that user can edit the extensions. A sandboxed (App Store) build could
  not run a downloaded Node at all.
- **The host starts only when an installed extension is enabled.** A call a page
  makes on every render (`entries`, `panelPages`, `drawerFor`) never starts it
  (`core::ext_host::answer_without_host`), or every page load would spawn Node.
- **The sidecar's stdout is the wire.** `main.js` sends `console.log` and stdout
  to stderr before anything loads; an extension's print would otherwise end the
  host.
- **Every host is forked on `ext_host::fork`'s one thread, which never exits.** On
  Linux `PR_SET_PDEATHSIG` fires when the forking *thread* exits: forked on a tokio
  blocking thread, the host was killed 10 s after launch.
- **A call past its deadline fails alone.** Only a missed `ping`, which `rpc.js`
  answers ahead of every handler, kills the host, and that kill blames no
  extension: three slow renders must not disable everything running.
- **Extension secrets use a keychain-only key** (`vault::keychain_only_key`),
  never a file beside them: with no keychain they are refused, as Electron's
  `safeStorage` refuses them. One read of it at a time (`VaultCache::reading`): a
  second, started while an unlock prompt holds the first, can create a second key.
- **A host call waits for `start`.** One that arrives while it runs and installs
  what it is loading would load that extension twice.
- **A hang blames the extension being loaded**, so three of them disable it, as a
  crash does; a hang with nothing loading blames none.
- **Tool-call reviews are asked in order and awaited apart** (`ask_running`, 32 at
  most). `call` awaits the Node check first, so concurrent calls lose their order.
  Only events with a pending tool call are sent (`awaits_review`), so others never
  fill the 32; past them a tool call skips the guardrails and the person answers it.
- **The panel bridge is Electron's `bridge.js`, run in the host; Rust only routes.**
  A page's send goes to `panelSend` with the address Rust read off the guest, never
  one the request names. `post` must count pages at once, so Rust keeps the host
  told which Extensions have a page open (`panelOpen`, re-sent to every new host).
  A post's script checks the page's `location` first, and a website gets fixed
  no-op receivers: the guest may have left for a site since its token was read.
- **The 1 MB message cap does not bound memory.** Tauri's scheme handler is
  handed the whole body, already read, so any page can make the shell buffer a
  large POST before it is refused; the protocol API offers no way to stop sooner.

## Releases, bundles and updater

- **Install-on-quit hooks `RunEvent::Exit`, never the cancellable `ExitRequested`.**
- **Tauri's default bundle names (`_amd64.AppImage`, `_aarch64`, `_x64.dmg`, spaces turned into dots by GitHub) are renamed in CI**
  to `AgentRQ-Tauri-<version>-<arch>.<ext>` (`collect-artifacts.mjs`).
