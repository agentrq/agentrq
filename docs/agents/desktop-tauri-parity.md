# Tauri parity checklist

> What proves that the Tauri build (`desktop-tauri/`) does what the Electron app
> does, on each OS. There is a row for every feature in [`docs/DESKTOP.md`](../DESKTOP.md)
> and for every member of the `window.agentrq` bridge. Read with
> [the Tauri note](desktop-tauri.md). Update a row in the same PR that changes
> what it covers.

## How to read it

Each OS cell says what shows the behaviour really works on that OS:

- **A**: automated, with the test or script named. A cell is automated only
  when the real behaviour runs there: a verify script, CI's screenshot job, or a
  unit test of logic that has no native part.
- **M*n***: manual. Step *n* of [the manual steps](#manual-steps) below.
- **G**: a gap, with the work package or follow-up that closes it.
- **—**: not applicable on that OS.

The **Tests** column lists the unit tests behind a row. CI runs them on Linux,
macOS and Windows (the `Desktop App (Tauri)` workflow's test job): `core::x` is
`desktop-tauri/core/src/x.rs`, `src-tauri::x` is `src-tauri/src/x.rs`, and a
`.test.js` is in `desktop-tauri/test/`. The verify scripts are in
`desktop-tauri/scripts/` and run on Linux only, in CI's `Verify scripts` job:
macOS has no WKWebView driver, and on Windows msedgedriver does not start the
app.

Open gaps that hold several rows: **WP8 release job** (#1035, no release
carries the Tauri build yet, so there is no update feed), and **WP10 part 2**
(the `verify-side-panel`, `verify-webmcp` and `verify-ui-telemetry` ports).

## Features

### Installing and updating

| Feature | Tests | Linux | macOS | Windows |
|---|---|---|---|---|
| One-command install (`install.sh --tauri`) | `installers/test/install.test.js` (asset picks) | M1; G WP8 release job | M1; G WP8 release job | — |
| Hand-downloaded packages | — | M2; G WP8 release job | M2; G WP8 release job | M2; G WP8 release job |
| First-launch security warning | — | — | M2 | M2 |
| Auto-update: check at launch and every 15 min, download | `core::updater` | M3; G WP8 release job | M3; G WP8 release job | M3; G WP8 release job |
| Update banner | `core::updater`, `bridge.test.js` (update state) | A `verify-e2e` (no banner without an update); M3 | M3 | M3 |
| Check for Updates… menu item | `core::menu` | G follow-up: the item is disabled (`can_check_for_updates: false`) | G same | G same |
| Script-install fallback | `core::updater`, `core::install_log` | M4 | M4 | — |
| Signed update feed (`latest-tauri.json`, minisign key) | `core::updater` | M3 | M3 | M3 |

### Connection, sign-in and profiles

| Feature | Tests | Linux | macOS | Windows |
|---|---|---|---|---|
| Connection screen, URL checked before saving | `core::server_config`, `src-tauri::commands` | A `verify-connection` | M5 | M5 |
| Self-signed certificate refused | — (reqwest's default verification) | M5 | M5 | M5 |
| `AGENTRQ_SERVER_URL` locks the connection | `core::server_config` | A (unit) | A (unit) | A (unit) |
| Config `agentrq-desktop.json`, v1/v2 migration | `core::server_config` | A (unit) | A (unit) | A (unit) |
| The `app` scheme and API proxy, cookie replayed | `core::protocol`, `src-tauri::scheme`, `src-tauri::network` | A `verify-e2e` | M9 | M9 |
| Request bodies flattened (attachments upload) | `net.test.js` (`installFetch`) | M9 | M9 | M9 |
| Live updates over `EventSource` | `net.test.js`, `src-tauri::event_source` | A `verify-e2e` (stream opens); M9 | M9 | M9 |
| Root-token sign-in | `core::auth` | A `verify-e2e` | M6 | M6 |
| Google and GitHub sign-in in the app | `core::auth`, `core::links`, `src-tauri::sign_in` | M6 | M6 | M6 |
| Session survives a restart (jar sealed with a private key file) | `src-tauri::cookie_jar`, `src-tauri::vault` | M6 | M6 | M6 |
| Log Out, Switch Server | `core::menu`, `src-tauri::sign_in` | M7 | M7 | M7 |
| Profiles: add, rename, switch, remove | `core::profiles`, `src-tauri::profiles`, `src-tauri::commands` | M8 | M8 | M8 |
| Profile storage kept apart | `src-tauri::profiles` | M8 | M8 | M8 |
| Account names, duplicate accounts | `core::identity`, `core::profiles` | A (unit) | A (unit) | A (unit) |
| Attachment cache, forgotten with its task | `core::attachment_store`, `src-tauri::commands` | A (unit) | A (unit) | A (unit) |

### Notifications, tray and links

| Feature | Tests | Linux | macOS | Windows |
|---|---|---|---|---|
| The shell's event stream (backoff, 401 stop, 75 s watchdog) | `core::sse`, `src-tauri::events` | A (unit) | A (unit) | A (unit) |
| Native notifications for agent activity | `core::notifications`, `src-tauri::notify` | A `verify-side-panel` (an agent's new task); M10 | M10 | M10 |
| Your own actions do not notify; 10 s dedupe | `core::notifications` | A (unit) | A (unit) | A (unit) |
| Mute per workspace | `src-tauri::commands` | M10 | M10 | M10 |
| Notification click opens the task | `src-tauri::shell` | A `verify-side-panel` (opens it); M11 (focuses the window) | M11 | M11; G follow-up: a click from the Action Center |
| Unread badge, cleared on focus | `core::notifications`, `src-tauri::shell` | M12 | M12 | M12 |
| Tray / menu-bar item | `core::tray` | M13 | M13 | M13 |
| Global shortcut `Cmd/Ctrl+Shift+N` | — | M14 | M14 | M14 |
| Deep links (`agentrq-tauri://`) | `core::deep_link` | M15 | M15 | M15 |
| Single instance | — | M15 | M15 | M15 |
| Web links: panel, modifier click, app-drawn, sign-in | `core::links` | M16 | M16 | M16 |

### Side panel

| Feature | Tests | Linux | macOS | Windows |
|---|---|---|---|---|
| Toggle (`Cmd/Ctrl+\`, task header, sidebar, title bar) | `core::menu`, `core::side_panel::wire` | M17; G WP10 part 2 | M17 | M17 |
| Address bar, back, forward, reload, stop | `panel-guest.test.js`, `src-tauri::side_panel` | M17, M20; G WP10 part 2 | M17, M20 | M17, M20 |
| Open in browser | `src-tauri::side_panel` | M17 | M17 | M17 |
| Expand, collapse, resize drag and reset | `panel-guest.test.js` (freeze while dragging) | M17, M19 | M17, M19 | M17, M19 |
| Task view in its phone layout beside the panel | (frontend tests) | M17 | M17 | M17 |
| Child webview placed over the host | `core::side_panel::child`, `panel-guest.test.js` | M17 | M17 | M17 |
| App menus, dialogs and toasts above the panel (snapshot) | `panel-guest.test.js` | M19 | M19 | M19 |
| Isolation: own store, never `at`, no capability (every command refused) | `core::side_panel::guest`, `parity.test.js` (every capability file), `src-tauri::panel_scheme` | M18; A `verify-side-panel` (a command is refused) | M18 | M18 |
| Popups and `target=_blank` | `core::side_panel::guest` | M17 | M17 | M17 |
| Cmd/Ctrl-click to the browser | `core::side_panel::guest` | M17 | M17, M24 | M17 |
| Menu keys with the focus in the panel | `panel-init.test.js` (WebView2 keys) | M21 | M21 | M21 |
| Focus moves between the app and the panel | `panel-guest.test.js` (focus) | M22 | M22 | M22 |
| A page that fails to load | — | M23 | M23 | M23 |
| Guest requests refused by the app's schemes | `src-tauri::scheme`, `src-tauri::panel_scheme`, `panel-init.test.js` | M25 | M25 | M25 |
| Find in page | `core::side_panel::wire`, `core::side_panel::find`, `src-tauri::side_panel` | A `verify-side-panel` (the bar's count, and each step by the page's own selection); M29 (the key in the page) | M29 | M29 |
| Site permissions bar, list, timeout | `core::side_panel::permissions`, `core::side_panel::permission_store`, `core::side_panel::platform_permissions`, `src-tauri::panel_permissions` | A `verify-side-panel` (location: the bar, Allow, remembered, listed); M26 | M26, M27 | M26, M28 |
| A site's notifications: asked, shown, clicked, named for the site, limited, refused once removed or blocked | `core::side_panel::platform_permissions`, `core::side_panel::site_notifications`, `src-tauri::notify`, `panel-init.test.js` | A `verify-side-panel` | M47 (G: WKWebView cannot show one; the site reads "denied") | M47; A the app's own window is refused by the shell, without a prompt (CI) |
| `agentrq-ext://` extension pages and their bridge | `core::side_panel::ext_scheme`, `core::side_panel::child`, `src-tauri::panel_scheme`, `src-tauri::side_panel`, `panel-init.test.js`, `ext-host-runtime.test.js` | A `verify-side-panel` | M25, M46 | M25, M46 |

### Files, clipboard and native helpers

| Feature | Tests | Linux | macOS | Windows |
|---|---|---|---|---|
| `file://` links: safe files open, others revealed | `core::files` | A `verify-markdown-links` | M30 | M30 |
| Copy-link button, shell clipboard | — | A `verify-markdown-links` (with `xclip`) | M30 | M30 |
| Folder chooser | — | M31 | M31 | M31 |
| Native emoji panel | `core::emoji_panel` | A (unit: answers `false`) | M32 | M32 |
| Dictation (microphone) | — | M33 | M33 | M33 |

### Window, menu and keys

| Feature | Tests | Linux | macOS | Windows |
|---|---|---|---|---|
| Window shown on first load, background follows the theme | `core::theme` | A CI screenshots (`pixel.mjs`); M34 | A CI screenshots; M34 | A CI screenshots; M34 |
| Window position and size kept, clamped to the displays | `core::window_state`, `core::normal_bounds`, `src-tauri::chrome_state` | M35 | M35 | M35 |
| Application menu | `core::menu` | M36 | M36 | M36 |
| Zoom and full screen | `core::window_view` | M36 | M36, M37 | M36 |
| Theme follows AgentRQ's setting | `core::theme` | A CI screenshots; M36 | A CI screenshots; M36 | A CI screenshots; M36 |
| macOS title bar: drag region, traffic lights, window menu | — | A `verify-connection` (no drag region off macOS) | M37 | — |
| Closing the window keeps the app; Dock reopens it | — | — | M38 | — |
| Menu keys run by the bridge, not native accelerators | `core::menu_keys`, `bridge.test.js` (menu keys) | M40 | — | M39 |
| A crashed webview reloads | `core::webview_crash` | M41 | G: WKWebView reports no crash through wry (documented) | M41 |
| Window and launcher icon, `StartupWMClass` | `core::app_icon` | M42 | — | — |
| Panic log in the app's log folder | `src-tauri::panic_log` | A (unit) | A (unit) | A (unit) |

### Extensions

| Feature | Tests | Linux | macOS | Windows |
|---|---|---|---|---|
| Install, run, uninstall; settings; supervisor sign-in; drawers; tool-call review | `ext-host-runtime.test.js`, `ext-host-examples.test.js`, `src-tauri::ext_host`, `src-tauri::extensions`, `src-tauri::oauth_window`, `src-tauri::events` | A `test/e2e/extensions.js` | M43 | M43 |
| `desktop/test/examples/*` and the examples through the bundled host, on the downloaded Node.js | `npm run test:sidecar` | A (CI's test job) | M43 | M43 |
| Node.js downloaded on first use, checked against its pin, offered again when the pin moves | `core::ext_node`, `src-tauri::ext_node`, `extensionCatalogue.test.js`, `extensionsViewNode.test.js` (frontend) | A `test/e2e/extensions.js` | M45 | M45 |

### Whole-app checks

| Check | Linux | macOS | Windows |
|---|---|---|---|
| Unit tests (vitest, `cargo test`) and the 100% line gates | A | A | A |
| `verify-e2e`, `verify-connection`, `verify-markdown-links` | A | M (no driver): M5, M6, M9, M30 | G follow-up: msedgedriver cannot start the app in CI; untried by hand |
| `verify-side-panel` | G WP10 part 2 | M17–M25 | M17–M25 |
| `verify-webmcp`, `verify-ui-telemetry` | G WP10 part 2 | M44 | M44 |
| `test/e2e/extensions.js`, in CI's test job | A | M43 | M43 |

## Bridge members

One row per member of `window.agentrq` (`src/bridge/index.js`) and the guest's
`window.agentrq.panel` (`src/guest/panel.js`). `parity.test.js` holds every row
to Electron's preload: the same members, the same kind and arity, one command
per channel, registered in all five places, with the arguments named as the
command takes them. `verify-e2e` checks on Linux that the whole bridge is there.

| Member | Command or event | Tests | Linux | macOS | Windows |
|---|---|---|---|---|---|
| `isDesktop`, `platform`, `versions` | — | `bridge.test.js` | A | A (unit) | A (unit) |
| `attachments.forgetWorkspace`, `forgetTask`, `forgetAll` | `attachments_forget_*` | `src-tauri::commands`, `core::attachment_store` | A (unit) | A (unit) | A (unit) |
| `connection.get`, `validate`, `save` | `connection_*` | `src-tauri::commands`, `core::server_config` | A `verify-connection` | M5 | M5 |
| `connection.cancel` | `connection_cancel` | `core::profiles` | M8 (the driver cannot see a second profile) | M8 | M8 |
| `notifications.get`, `setMuted` | `notifications_*` | `src-tauri::commands` | M10 | M10 | M10 |
| `profiles.get`, `switch`, `add`, `rename`, `remove` | `profiles_*` | `src-tauri::commands`, `src-tauri::profiles`, `core::profiles` | M8 | M8 | M8 |
| `files.open` | `files_open` | `core::files` | A `verify-markdown-links` | M30 | M30 |
| `clipboard.write` | `clipboard_write` | — | A `verify-markdown-links` | M30 | M30 |
| `emoji.showPanel` | `emoji_show_panel` | `core::emoji_panel` | A (unit) | M32 | M32 |
| `dialog.chooseDirectory` | `dialog_choose_directory` | — | M31 | M31 | M31 |
| `navigation.onNavigate` | `agentrq:navigate`, `navigation_ready` | `bridge.test.js` | M11, M14, M15 | M11, M14, M15 | M11, M14, M15 |
| `extensions.state`, `refresh`, `chooseFolder`, `installLocal`, `installFromCatalogue`, `uninstall`, `setEnabled`, `configure`, `setHookConsent`, `supervisor`, `authorize`, `deauthorize`, `entries`, `drawer`, `invoke` | `extensions_*` | `src-tauri::extensions`, `core::ext_host` | A `test/e2e/extensions.js` | M43 | M43 |
| `extensions.downloadNode`, `cancelNodeDownload`, `onNodeProgress` (Tauri only: Electron bundles Node) | `extensions_download_node`, `extensions_cancel_node_download`, `agentrq:extensions:node-progress` | `src-tauri::ext_node`, `src-tauri::extensions`, `core::ext_node`, `parity.test.js` | A `test/e2e/extensions.js` | M45 | M45 |
| `extensions.onChanged` | `agentrq:extensions:changed` | `parity.test.js`, `src-tauri::extensions`, `src-tauri::ext_host` | M43 | M43 | M43 |
| `updates.get`, `onStatus` | `update_get`, `agentrq:update:status` | `core::updater`, `src-tauri::commands`, `bridge.test.js` | A `verify-e2e` (no banner); M3 | M3 | M3 |
| `updates.check` | `update_check` | `core::updater`, `src-tauri::commands` | G follow-up: menu item disabled | G same | G same |
| `updates.installNow` | `update_install` | `core::updater` | M3 | M3 | M3 |
| `updates.installViaScript` | `update_install_via_script` | `core::updater`, `core::install_log` | M4 | M4 | — |
| `sidePanel.guest` | — (`'tauri'`) | `parity.test.js` | A (unit) | A (unit) | A (unit) |
| `sidePanel.onToggle` | `agentrq:side-panel:toggle` | `core::menu` | M17 | M17 | M17 |
| `sidePanel.onOpen` | `agentrq:side-panel:open` | `src-tauri::side_panel` | M16 | M16 | M16 |
| `sidePanel.onFind` | `agentrq:side-panel:find` | `core::side_panel::wire` | M29 | M29 | M29 |
| `sidePanel.pages` | `side_panel_pages` | `src-tauri::commands` | A `test/e2e/extensions.js` | M43 | M43 |
| `sidePanel.openExternal` | `side_panel_open_external` | `src-tauri::side_panel` | M17 | M17 | M17 |
| `sidePanel.onPermissionRequest`, `onPermissionSettled` | `agentrq:side-panel:permission-*` | `core::side_panel::permissions`, `src-tauri::panel_permissions` | M26 | M26, M27 | M26, M28 |
| `sidePanel.answerPermission`, `permissions`, `removePermission` | `side_panel_permission_answer`, `side_panel_permissions`, `side_panel_permission_remove` | `core::side_panel::permission_store`, `src-tauri::panel_permissions` | M26 | M26, M27 | M26, M28 |
| `view.get`, `zoom`, `toggleFullScreen` | `view_get`, `view_zoom`, `view_fullscreen` | `core::window_view` | M36 | M36, M37 | M36 |
| `theme.set` | `theme_set` | `core::theme` | A CI screenshots; M36 | A CI screenshots; M36 | A CI screenshots; M36 |
| `panel.send`, `panel.onMessage` (guest) | `POST /.agentrq/panel-send` on `agentrq-ext`; a post is evaluated into the page | `panel-init.test.js`, `src-tauri::panel_scheme`, `src-tauri::side_panel`, `ext-host-runtime.test.js` | A `verify-side-panel`; M25 | M25, M46 | M25, M46 |

The bridge's own commands, which Electron has no channel for:

| Member | Command | Tests | Linux | macOS | Windows |
|---|---|---|---|---|---|
| `EventSource` for same-origin streams | `event_source_open`, `event_source_close` | `net.test.js`, `src-tauri::event_source`, `src-tauri::commands` | A `verify-e2e` | M9 | M9 |
| `fetch` with flattened bodies | — | `net.test.js` | M9 | M9 | M9 |
| Menu keys | `menu_key_bindings`, `menu_run` | `bridge.test.js`, `core::menu_keys` | M40 | — (native menu) | M39 |
| Side-panel guest | `side_panel_guest_open`, `_place`, `_navigate`, `_snapshot`, `_focus`, `_close` | `panel-guest.test.js`, `src-tauri::side_panel` | M17–M23 | M17–M24 | M17–M23 |
| Side-panel find | `side_panel_guest_find`, `side_panel_guest_stop_find` | `core::side_panel::find`, `src-tauri::side_panel` | M29 | M29 | M29 |

## Manual steps

Run each step on the OS its tag names; untagged steps run on all three. Use a
build installed as a person would install it (`install.sh --tauri` or a
package), not `tauri dev`: on macOS notifications and deep links need the real
bundle id, and on Windows notification clicks need the installed app's AUMID.
Keep the Electron app installed beside it, to compare. Record each result, with
screenshots, in the PR or task that ran the checklist.

### Installing and updating

1. **[macOS, Linux]** `curl -fsSL https://agentrq.com/install.sh | sh -s -- --tauri`
   installs AgentRQ Tauri beside an installed Electron AgentRQ; both start.
   Running it again updates in place; `--quit` closes a running preview first.
   On Linux, the launcher entry `agentrq-tauri.desktop` shows the icon and
   `agentrq-tauri://` links open the app.
2. Each package installs and starts: `.dmg` for arm64 and x64, the per-user
   `.exe` for Windows x64, `.deb` and `.AppImage` for Linux x64. A
   hand-downloaded build shows the first-launch warning as `docs/DESKTOP.md`
   describes.
3. Update from N to N+1 with a test release (a workflow_dispatch on a fork, or a
   `v0.0.0-tauri-test` draft deleted afterwards): an installed build N finds,
   downloads and installs N+1, and shows the banner with **Update now**. The
   same release still updates the Electron app. The first signed `build-tauri`
   run proves the `TAURI_SIGNING_PRIVATE_KEY` and password secrets.
4. **[macOS, Linux]** Where the banner offers the script install (an unsigned
   macOS build), **Update now** runs `install.sh`, logging to a file and never
   a pipe, quits, installs and reopens the app. On Linux the reopen works
   (`open -a` is macOS only).

### Connection, sign-in and profiles

5. First run shows the connection screen with the URL field focused. A bad URL
   is refused with a reason and nothing is stored; a good one is saved and the
   login page loads through the proxy. A server with a self-signed certificate
   is refused, with no way to ignore it.
6. Sign in with a root token, Google and GitHub. The sign-in window stays in
   the app, never the system browser, closes itself, and the main window
   reloads signed in. Closing the sign-in window counts as a failure. Quit and
   start again: still signed in (the jar is sealed with `cookie.key`, a file
   only this user can read). No keychain prompt appears.
7. **Log Out** clears the session and keeps the server; **Switch Server** goes
   back to the connection screen (the AgentRQ menu on macOS, File elsewhere).
8. Profiles:
   - Add a second profile on another account: the connection screen offers a
     way back. Switch both ways; the switcher names both accounts. Rename one.
   - Remove one: its storage (`profiles/<id>/`, or its macOS data store) and its
     panel cookies are gone. The last profile cannot be removed.
   - **[Windows]** A switch builds the new window in the run loop's `Destroyed`
     event: the app never quits, and no second window flashes up.
   - **[macOS 14+]** Each profile's localStorage stays its own
     (`data_store_identifier`).
   - **[macOS 13 or older]** Each profile is on its own origin,
     `app://<profileId>/`: localStorage and IndexedDB stay apart and survive a
     restart.
9. Signed in: an agent's change to a task shows without a reload, and the
   stream comes back after the server restarts. Attach a file to a message and
   it uploads whole (a Blob body crashes WebKitGTK and arrives empty on
   WKWebView unless the bridge flattens it).

### Notifications, tray and links

10. With the window in the background, an agent's reply, new task and status
    change each raise a native notification, worded as the web push is. Your
    own actions raise none. A workspace muted under **Workspace Settings →
    Notifications → Desktop Alerts** raises none.
11. Clicking a notification focuses the window and opens the task.
    **[Windows]** Only while the toast is on screen; a click from the Action
    Center does nothing yet.
12. The unread count shows on the Dock (macOS), the launcher (Linux, where the
    desktop shows counts) or as the taskbar's overlay icon (Windows), and clears
    when the window takes focus.
13. The tray shows the unread status, Open, New Task, the five most recent
    workspaces and Quit, with the count in its tooltip. Clicking it focuses the
    window.
14. `Cmd/Ctrl+Shift+N` from another app opens New Task.
15. `agentrq-tauri://workspaces/<id>/tasks/<id>` opens the task from a cold
    start and from a running app, where the second launch hands over and exits.
    So do the `events` and `workflows` forms, with their query kept.
16. A web link in a message opens in the side panel. Cmd/Ctrl-click and
    middle-click open the system browser. The app's own links (docs, terms)
    open the system browser. An AgentRQ sign-in link stays in the app.

### Side panel

17. The checks of `desktop/scripts/verify-side-panel.mjs`, by hand, apart from
    find, permissions and extension pages:
    - The panel starts closed; `Cmd/Ctrl+\`, the task header button, the
      sidebar's **Panel** button and (macOS) the title-bar button open it.
    - Typing an address loads it, and the address field follows the page.
    - A new-window link and a `target=_blank` link open in the panel, with no
      second window. A page cannot navigate the panel to a file.
    - Unresized, the panel leaves the main column a phone's width, and the task
      view uses its phone layout. Dragging the edge resizes it, the width is
      remembered, and a double-click resets it.
    - **Open in browser** hands the page to the system browser.
    - The task header's panel button hides while the panel is open.
    - **Expand** takes everything beside the sidebar and keeps the task;
      collapsing brings it back; close restores the full layout.
18. Sign in to a site in the panel: its cookie stays in the panel, and the site
    never sees the AgentRQ session. The panel's cookies survive a restart.
    **[macOS 13 or older]** The panel is incognito: they do not.
19. Open an app menu, a dialog and a bottom-right toast while the panel shows a
    page: each is drawn above the panel (the panel is swapped for its snapshot)
    and the live page comes back after. Dragging the panel's edge never stalls
    over the page.
20. Back and forward step through the panel's history, including in-page
    navigations; reload and stop work.
21. With the focus in the panel, `Cmd/Ctrl+\`, zoom and reload run once; a held
    zoom key repeats, a held reload does not; a key the page handles itself
    stays with the page. **[Windows]** In a cross-origin iframe inside the
    panel's page, note whether a menu key runs its action once, never or
    twice: WebView2's `WebMessageReceived` hears only the top frame.
22. Click into the panel's page: the address field follows it and the app's own
    page loses the focus. Click back into the app: the focus returns. Check
    this on macOS above all.
23. An address that cannot load shows the failure in the panel, never a blank
    or stuck panel. Check this on Windows above all, whose load-failure path
    differs.
24. **[macOS]** Within a second of a Cmd-click elsewhere in a panel page (a
    multi-select, say), a subframe load or a script's `location =` is not sent
    to the system browser. The Cmd-click is read from `NSApp.currentEvent`, not
    the navigation, so a real fix needs a `decidePolicyForNavigationAction`
    swizzle or a wry change.
25. From a web page in the panel, a `fetch` to
    `agentrq-ext://…/.agentrq/panel-send` is refused, and `app://`,
    `tauri://localhost` and `http://tauri.localhost` answer 403. Note the
    `Origin` each request carried: WebKit sends none on a same-origin
    custom-scheme request, which is why the guest's token is required.
26. Open webcamtests.com in the panel:
    - A camera request shows the bar once. **Allow** is remembered after a
      restart (`side-panel-permissions/<profileId>.json`).
    - **Site permissions** lists the site and removes it, and the next visit
      asks again.
    - With no answer for 60 s the page gets a Block; leaving the page answers
      Block and takes the bar down.
27. **[macOS]**
    - A guest no longer gets the camera without a question. wry grants it when
      no handler is set; the app replaces wry's `requestMediaCapturePermission…`
      for guests only, and the app's own window keeps wry's answer.
    - There is no request before macOS 12. Geolocation, notifications and
      screen capture have no public WKWebView hook (documented); only the
      camera and microphone are asked about.
28. **[Windows]** The request is held by a deferral with
    `SavesInProfile(false)`. WebView2 shows no prompt of its own, and a
    removed answer really asks again. Autoplay is granted without asking.
    One `getUserMedia({video, audio})` asks one camera and microphone question
    (WebView2 raises two requests; halves within 250 ms are merged), and its
    answer gives both tracks.
29. Find:
    - `Cmd/Ctrl+F` in the panel's page opens the panel's find bar, and the page
      never sees the key. Matches are highlighted and counted; `Enter` and
      `Shift+Enter` step both ways and wrap around; `Esc` closes the bar.
    - **[macOS]** Find uses `findString:withConfiguration:` plus a script count:
      check the count, and that a new search starts at the first match. `Cmd+F`
      is caught by an `NSEvent` local monitor: it opens the find bar only when
      the focus is in the panel, never instead of the app's own `Cmd+F`.
    - **[Windows]** Find uses `ICoreWebView2Find` (runtime 1.0.3240+): check
      that `ActiveMatchIndex` is 0-based, or the bar shows one too low, and that
      the `window.find` fallback works on an older runtime. `Ctrl+F` is
      `Handled` in `AcceleratorKeyPressed`: the page's own keydown never fires,
      and only the app's find bar shows, never WebView2's own
      (`SuppressDefaultFindDialog`).

### Files, clipboard and native helpers

30. **[macOS, Windows]** The checks of `verify-markdown-links`, by hand: a
    document link opens in its app, quietly; a script is shown in Finder or
    Explorer, never run, and the app says so; a folder opens; a missing file is
    reported by name; the copy button copies the path and says so.
31. The working-directory field of a workspace opens the native folder dialog,
    in front of the window, and it can create a folder.
32. **[macOS, Windows]** The emoji button of a reaction opens the system's
    emoji picker (the character palette on macOS, Win+. on Windows).
33. Dictation asks for the microphone (once, on macOS) and then works.

### Window, menu and keys

34. Screenshots in light and dark, beside Electron's at the same size. Starting
    the app shows no white flash.
35. The window's position, size and maximized state survive a restart. Unplug
    the display it was on: it comes back on one you have.
36. Every menu item works: on macOS the app menu, elsewhere File, Edit, View,
    Window and Help. Devtools shows only in a debug build. Zoom in, out and
    reset, and full screen work. Changing AgentRQ's theme changes the window's
    chrome and background.
37. **[macOS]** The window drags from the title text and from the empty title
    strip, and the buttons on it click without dragging. The traffic lights sit
    in their place. The title bar's window menu zooms and toggles full screen.
38. **[macOS]** Closing the window keeps the app in the Dock, and clicking the
    Dock icon opens it again (`prevent_exit`). `Cmd+Q` saves the window state.
39. **[Windows]** Each menu key fires exactly once: reload (`Ctrl+R`) and zoom
    (`Ctrl+=`, `Ctrl+-`, `Ctrl+0`). WebView2's own `F5`, `Ctrl+F`, `Ctrl+P`,
    `Alt+Left`, `Alt+Right` and `F12` do nothing, as in Electron.
40. **[Linux]** In a page that handles them (the web terminal), `Ctrl+W`, `R`,
    `Z`, `Q`, `M`, `0` and `-` stay with the page; elsewhere each runs its menu
    action once.
41. **[Linux, Windows]** Kill the webview's process (`WebKitWebProcess`,
    `msedgewebview2`): the window reloads. A fourth crash within a minute is
    left alone.
42. **[Linux]** The window and the dock show the AgentRQ icon, and the launcher
    groups the window under its entry (`StartupWMClass` `agentrq-tauri`).

### Extensions and the rest

43. The five examples in `examples/extensions/` install, run and uninstall;
    `npm run test:sidecar` passes on the Node.js the app downloaded; a
    guardrail denies a tool call; the supervisor's authorisation window opens
    on the consent page without a sign-in, and authorising closes it with
    nothing handed to the system (no `agentrq://` prompt); the drawers open.
    An extension that crashes the host three times is turned off, and the
    Extensions page says why.
44. **[macOS, Windows]** The checks of `desktop/scripts/verify-webmcp.mjs` and
    `verify-ui-telemetry.mjs`, by hand: the WebMCP tools appear in
    `document.modelContext`, run, and go on sign-out; a keypress, a copy, a
    search and a view switch each send a UI telemetry report for their
    workspace.
45. **[macOS, Windows]** On a fresh profile, the Extensions page offers Node.js
    with its size; Download fills the bar, Cancel stops it, and once it is
    in place an example installs and runs. macOS: the downloaded `node` runs
    as a child of the signed, hardened app. Windows: the rename succeeds with
    an antivirus scanning `node.exe`.
46. **[macOS, Windows]** Open the panel-notes example's page from its task
    menu (**Add to notes**): the panel opens at it, typed notes are saved and
    come back after a reload, and adding another task updates the open page.
    Windows: the page's address is `http://agentrq-ext.panel-notes/…`.
47. **[macOS, Windows]** A site's notifications in the panel. Use a page that
    asks on a click (`Notification.requestPermission()`), then shows
    `new Notification(...)` with an `onclick`. Re-run 10 and 11 too: the app's
    own notifications go through the same plugin, now registered without its
    page script.
    - **[Windows]** The click shows the bar, and **Allow** gives `granted`.
      The notification shows as WebView2's own toast (runtime 128+), and
      clicking it runs the page's `onclick`. CI checks that the app's own
      page is refused with no prompt; also click the Workspace settings'
      **Enable Push** and see none.
    - **[macOS]** First note `typeof Notification` in a guest: if it is
      `undefined`, nothing shows and the rest of this step is moot.
      Otherwise `Notification.permission` and
      `navigator.permissions.query({name: 'notifications'})` read `denied`,
      `requestPermission()` resolves `denied` with no bar, nothing shows, and
      an `<iframe>` in the page reads the same.
