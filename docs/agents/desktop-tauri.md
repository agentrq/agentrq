# Tauri desktop app (`desktop-tauri/`, preview)

> Read before changing anything under `desktop-tauri/`. It renders desktop's
> renderer unchanged, so [the desktop note](desktop.md) applies here too.

- **Pure logic goes in `core/`**, which has no Tauri dependency and a 100% line
  gate. `src-tauri/src/lib.rs` is wiring only and is outside the gate.
- **A bridge command is named after its Electron channel** (`agentrq:side-panel:permission-answer`
  is `side_panel_permission_answer`). `commands.rs` holds one per channel, as a
  `todo` stub until its work package replaces it.
- **`bridge/index.js` mirrors `desktop/src/preload/index.js` member for member**, and
  `test/parity.test.js` loads both. Change the preload and the bridge follows, or CI
  fails. A call's arguments cross as one object keyed by the preload's parameter names.
- **A new command is five places:** `commands.rs`, its `generate_handler!`, `COMMANDS` in
  `build.rs`, `capabilities/main.json` (`allow-<name-with-dashes>`) and the bridge. Naming
  the commands in `build.rs` is what makes Tauri deny any other, so a command missing
  from the capability fails at runtime, not at build. The parity test compares all five.
- **Scope the capability with `webviews`, never `windows`.** A window grant also covers
  every child webview in it, so WP7b's side-panel guests and `agentrq-ext:` pages would
  inherit all our commands. Guests get no capability; a test reads every capability file.
- **`window.agentrq` is deep-frozen and non-writable** (`versions` stays mutable until the
  Tauri version resolves), as Electron's contextBridge leaves it. Page code must not be
  able to replace a member.
- **Flatten request bodies in the bridge's `fetch` wrapper.** A Blob or File body
  sent to the `app` scheme segfaults WebKitGTK and arrives empty on WKWebView.
  Same-origin Blob, FormData and stream bodies go as an ArrayBuffer.
- **Every server call goes through the active profile's client** (`network.rs`):
  its jar is the session. A shared or default client signs two profiles in as one.
- **Every proxied or cached response carries `nosniff` and `sandbox` (core's
  `PROXIED_RESPONSE_GUARDS`).** It is served on the app's own origin, so a framed
  HTML attachment would otherwise run as the app and reach `__TAURI_INTERNALS__`.
- **A cookie jar with no vault never writes its file.** A slow or refused
  keychain leaves the session in memory; a jar resealed with a stand-in key
  would sign the person out for good.
- **With no vault, only the active profile's network is kept** (`Network::open`).
  Any other is opened afresh on each use: its jar can never read its file, so a
  kept one would show that profile signed out for the rest of the session.
- **The proxy buffers; an `EventSource` goes over IPC.** The scheme answers only
  once it has the whole body, so an endless stream through it never answers.
- **Same-origin in the bridge compares scheme and host, never `origin`**: a
  custom scheme's origin is opaque (`"null"`).
- **Tauri's asset resolver answers `index.html` for any missing path.** Ask the
  embedded keys whether a file exists, or `plan_static_request`'s 404 never fires.
- **Read `at` with `cookies()` and our own domain-match, never `cookies_for_url`.**
  On macOS it matches the domain exactly, so it misses an IP host and the
  production `Domain=.<domain>` cookie.
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
- **A sign-in window moves every cookie for the server's host into the jar**,
  not only `at`. The refresh cookie `rt` is what keeps the session past a day.
  The sign-in window gets no capability.
- **Delete a webview cookie as both `host` and `.host`** (`sign_in::deletion_forms`).
  `cookie`'s `domain()` strips the leading dot and the stores match exactly, so
  passing back what `cookies()` gave deletes nothing.
- **A profile's webview storage lives under `profiles/<id>/`** (or its macOS
  data store), and start-up sweeps any profile folder or store that no profile owns.
- **The connection probe uses a client with no cookies that follows
  redirects**, never a profile's client. A probe must not sign anyone in.
- **Windows' `delete_cookie` is asynchronous.** Check the deletion after the next
  navigation, never straight after the call.
- **On Linux, Tauri stacks child webviews in a GtkBox**, so the side panel lands
  below the page. Put the main webview in a GtkOverlay and place the child with
  `get-child-position`.
- **Once a window has a child webview, `get_webview_window` finds nothing**, and a
  `WebviewWindow` command argument fails: Tauri sees a webview window only while
  every webview shares the window's label. Use `chrome::main_webview` and a
  `Webview` argument.
- **Every webview but `main` gets 403 from the `app` scheme**, and only a
  `panel-guest-*` gets anything from `agentrq-ext`: schemes are app-wide. A guest
  has no capability, so its bridge is a `POST` to `/.agentrq/panel-send` on
  `agentrq-ext`, routed by the guest's own URL. It must carry the guest's token,
  which Rust pushes to the top frame on each load: WebKit sends no `Origin` on a
  same-origin custom-scheme request, so a missing `Origin` proves nothing.
- **The app registers its own `tauri` scheme, a 403**, so no guest reaches the
  built-in asset server (`tauri://localhost`, `http://tauri.localhost`).
- **Every guest is destroyed when `main` starts a load** (reload, crash, sign-out,
  profile switch); the new page knows none of them.
- **WebKit makes no new window of a Cmd/Ctrl-click**, unlike Chromium, and wry
  reports no keys: the guest reads them natively in `on_navigation`, and only for
  a user's link click (a GLib emission hook on `decide-policy`, since wry's handler
  stops the signal), so a script's redirect never opens the browser.
- **Before macOS 14 a guest is `incognito`**: wry ignores `data_store_identifier`
  there and the guest would share the default store.
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
- **Drag regions are `data-tauri-drag-region="deep"`, and `="false"` on every
  `.app-no-drag`.** A bare attribute drags from that element only, not from the
  title text inside it, and a no-drag card of plain divs would drag without `"false"`.
- **Never write localStorage and quit in the same tick.** One such write was lost
  on macOS 14.
- **`make stop` must leave the Tauri dev app alone**: it skips any vite under
  `desktop-tauri/`, and `tauri dev` exits with its Vite (port 5175).
- **Zoom goes through `core::window_view` and `set_zoom`, never a native zoom
  shortcut.** Tauri cannot read a webview's zoom back, so `ZoomTable` is the only
  record; a native zoom would leave it, and the title bar's menu, wrong.
- **Never use `tauri-plugin-window-state`.** Its clamping differs from
  `core::window_state`. The save is read when the debounce fires, never per
  resize event: a platform can report the maximized size before the state.
- **`chrome.rs`, `app_menu.rs`, and `side_panel/`'s platform files and `handlers.rs`
  are wiring and outside the coverage gate**, like `lib.rs`; a rule goes in `core/`
  or a gated file with its test. `side_panel.rs` is gated: llvm-cov counts a closure
  per generic instance, so keep closures out of its commands. An async command whose
  arguments cannot fail (`AppHandle`, `Webview`) leaves its attribute line uncovered:
  take a `State`, which fails when unmanaged, and test that.
- **The window starts hidden and is shown on the page's first load** (5 s
  fallback), so a failed load still appears. Showing earlier is the white flash.
- **A webview process crash reloads the window** (WebKitGTK, WebView2), at most 3
  times a minute. WKWebView reports none through wry, so macOS cannot.
- **Off macOS the menu has no native accelerators.** GTK runs a window's accelerators
  before the page, so Ctrl+W closed the app under a terminal that wanted it, and Ctrl+R,
  Z, Q, M, 0 and - went the same way. The label keeps a hint; the bridge runs the key
  from a bubbling `keydown` that is not `defaultPrevented`, checked a task later so a
  page listener on `window` can cancel it, against the table in `core::menu_keys`
  (Electron's rule). The table leaves out undo/redo, which the webview does itself, and
  says which keys repeat (zoom only); `menu_run` takes only an id that has a key here and
  now. A key's physical place is used only for a letter of a non-Latin script, never for
  `ß`, `¡`, `´` or a dead key. WebView2's own accelerator keys are switched off, or Ctrl+R
  would reload twice, in a side panel guest too. Windows is otherwise unmeasured. A
  guest's keys never reach that listener: on Linux the GtkOverlay takes the keys WebKit
  sends up unhandled; on Windows `AcceleratorKeyPressed` only notes the key and the
  guest's init script posts it if the page left it.
- **A command's arguments are required unless the page may leave them out.** An `Option`
  turns a key the bridge misspelled (`fileUrl` for `rawUrl`) into `None`, silently.
  The parity test checks every implemented command's parameter names against the bridge.
- **On macOS `ExitRequested { code: None }` must `prevent_exit()`**, or closing the
  window quits and the Dock's reopen never runs. Cmd+Q and Windows' Exit raise only
  `Exit`, so the state is saved there too.
- **`AGENTRQ_E2E_INIT_SCRIPT` is CI's**: a script the main webview runs first, which seeds
  the theme until WP4 lets the renderer reach the app (`test/e2e/`). Only a debug build or
  `--features e2e` reads it; a release must not run an environment's script in the webview.
