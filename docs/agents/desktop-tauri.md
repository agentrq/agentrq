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
  inherit all our commands. Guests get a capability of their own, keyed on their labels.
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
- **The proxy buffers; an `EventSource` goes over IPC.** The scheme answers only
  once it has the whole body, so an endless stream through it never answers.
- **Same-origin in the bridge compares scheme and host, never `origin`**: a
  custom scheme's origin is opaque (`"null"`).
- **Tauri's asset resolver answers `index.html` for any missing path.** Ask the
  embedded keys whether a file exists, or `plan_static_request`'s 404 never fires.
- **Read `at` with `cookies()` and our own domain-match, never `cookies_for_url`.**
  On macOS it matches the domain exactly, so it misses an IP host and the
  production `Domain=.<domain>` cookie.
- **Windows' `delete_cookie` is asynchronous.** Check the deletion after the next
  navigation, never straight after the call.
- **On Linux, Tauri stacks child webviews in a GtkBox**, so the side panel lands
  below the page. Put the main webview in a GtkOverlay and place the child with
  `get-child-position`.
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
- **`chrome.rs` and `app_menu.rs` are wiring and outside the coverage gate**, like `lib.rs`; a rule
  goes in `core/` or `chrome_state.rs` with its test.
- **The window starts hidden and is shown on the page's first load** (5 s
  fallback), so a failed load still appears. Showing earlier is the white flash.
- **A webview process crash reloads the window** (WebKitGTK, WebView2), at most 3
  times a minute. WKWebView reports none through wry, so macOS cannot.
- **Off macOS the menu has no native accelerators.** GTK runs a window's accelerators
  before the page, so Ctrl+W closed the app under a terminal that wanted it, and Ctrl+R,
  Z, Q, M, 0 and - went the same way. The label keeps a hint; the bridge runs the key
  from a bubbling `keydown` that is not `defaultPrevented`, checked a task later so a
  page listener on `window` can cancel it, against the table in `core::menu_keys`
  (Electron's rule). A held key is ignored but for zoom, undo/redo are the webview's
  own, and `menu_run` takes only an id that has a key here and now. Windows is treated alike, unmeasured. A guest
  page's keys (WP7b's side panel) never reach it, so the guest must forward them.
- **A command's arguments are required unless the page may leave them out.** An `Option`
  turns a key the bridge misspelled (`fileUrl` for `rawUrl`) into `None`, silently.
  The parity test checks every implemented command's parameter names against the bridge.
- **On macOS `ExitRequested { code: None }` must `prevent_exit()`**, or closing the
  window quits and the Dock's reopen never runs. Cmd+Q and Windows' Exit raise only
  `Exit`, so the state is saved there too.
- **`AGENTRQ_E2E_INIT_SCRIPT` is CI's**: a script the main webview runs first, which seeds
  the theme until WP4 lets the renderer reach the app (`test/e2e/`). Only a debug build or
  `--features e2e` reads it; a release must not run an environment's script in the webview.
