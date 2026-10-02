# Desktop side panel

Task: 0jfr3oIYfkf · 2026-10-02 · Desktop app only

## What it is

A resizable panel on the right of the desktop window that shows a web page, or
a page an Extension ships. Like the in-app browser in Codex: a link in a task
opens next to the task instead of in another application, and an Extension has
one place where it can draw anything at all — its own HTML, CSS and JS —
instead of the closed node vocabulary every other surface uses.

The web build does not change.

## Decisions taken with the human

| Question | Answer |
|---|---|
| What the panel shows | Web pages **and** Extensions' own pages |
| A plain click on an http(s) link in a task or message | Opens in the panel; **Cmd/Ctrl-click** opens the system browser |
| Can an Extension's page talk to its own Extension code | **Yes**, through a message bridge |
| How it is embedded | A `<webview>` in the page, not a `WebContentsView` over the window (below) |

## Why a `<webview>`, not a `WebContentsView`

A `WebContentsView` is a native layer drawn *over* the window, so nothing the
renderer draws can appear above it. The renderer has about 25 full-window
overlays (modals, menus, the command palette) and its toasts sit bottom-right —
exactly where the panel is. Each one would be half-hidden behind the panel, and
keeping a native view's bounds in step with a CSS layout during a drag is a
second problem of its own.

A `<webview>` is an element: it is laid out, resized and covered like any other.
Electron's caution about it is about the embedder handing it power. Here the
main process owns every guest's preferences through `will-attach-webview`
(below), which is the boundary.

Proven on Electron 44.3 before writing this (a throwaway spike, run headless):
a `<webview>` loads both `https://` and a custom privileged scheme from an
`app://` page whose CSP is `default-src 'self'` — the embedder's policy does not
apply to a guest — and `ipcMain` sees the guest's `event.senderFrame.url`.

## Layout

- The panel is the last column of the row in `App.vue`, after `<main>`, shown
  only when `platformStore.isDesktop` and the panel is open.
- **Toolbar** in the app's own components: back, forward, reload/stop, an
  address field (Enter navigates; a bare word or anything without a scheme gets
  `https://`), *Pages* (a menu of the installed Extensions' panel pages), *Open
  in browser*, close. The title of an Extension page is followed by the
  Extension's name, as every other Extension surface is.
- **Width.** Unresized, the panel takes all the room except 480px — a phone's
  width — for the main column. Views with a list-and-detail split (a
  workspace's tasks, the task inbox) and the task view lay themselves out by
  their own width (`useNarrowLayout` plus Tailwind container variants), so
  beside the panel a task looks as it does on a phone, and returns to the
  desktop layout when the panel closes.
- **Resize** by dragging the panel's left edge (a 16px handle with an
  always-visible grip, `cursor: col-resize`). Minimum 320px, and the main
  column keeps at least 480px when there is room for both; a dragged width is
  clamped to the room at draw time and kept as chosen. While dragging, the
  webview gets `pointer-events: none`, otherwise the guest swallows the
  pointer the moment it crosses into it and the drag stalls. Double-clicking
  the handle goes back to filling the room.
- **Expand** (beside close) gives the panel everything beside the sidebar; the
  main column is hidden, not unmounted, so the page is as it was on
  *Collapse*.
- **Remembered** per device in `localStorage` (`agentrq:side-panel`: open,
  full, width — `null` for "fill" — and last URL), read inside try/catch. Only an `http(s):` or
  `agentrq-ext:` URL is restored. It is a layout preference, so it is not
  account state.
- **Toggle**: `CmdOrCtrl+\` from a *View → Side Panel* menu item (every
  platform, works whatever has focus), a button in the task view's header, a
  button in the macOS title bar beside the window menu, and a sidebar footer
  button on every desktop platform. The
  menu item sends `agentrq:side-panel:toggle` to the renderer.
- Empty state (opened with nothing loaded): a short line, the address field
  focused, and the Pages list if any Extension provides one.
- Load failure (`did-fail-load`, ignoring aborted `-3`): the error in the panel
  with *Retry* and *Open in browser*.

## Links

- `App.vue`'s existing delegated click handler gains one step on desktop, after
  copy and `file:` links: a plain primary click on an `<a href>` that is
  `http(s):` and not an AgentRQ sign-in URL opens in the panel. With Cmd/Ctrl,
  Shift or a middle click it is left alone, so it reaches the window-open
  handler and the system browser exactly as today.
- Sign-in links, `mailto:` and friends, and refused schemes (`file:`,
  `javascript:`, `data:`) keep today's `classifyLink` handling.
- **Inside the panel**: navigation stays in the panel for `http(s):` and
  `agentrq-ext:`; any other scheme goes through `classifyLink` (so `mailto:`
  reaches the OS and `file:` is refused). A `target=_blank`/`window.open` opens
  in the panel, or in the system browser when the disposition says the user
  asked for a new window/tab with a modifier. A guest never opens a second
  window.
- An AgentRQ sign-in URL in the panel is sent to the main window's sign-in flow
  rather than loaded: the panel's cookie jar is not the profile's, so a sign-in
  there would "succeed" and leave the app signed out.

## The guest, and why it is safe

The main window gets `webviewTag: true`. Its `will-attach-webview` handler is
the only place a guest's preferences are decided, and it overwrites whatever
the element asked for:

- `partition` → `persist:panel-<profileId>`: one jar per profile, never the
  profile's own partition, so no page in the panel can ever carry the `at`
  cookie. Forgetting a profile clears its panel partition too.
- `nodeIntegration: false`, `nodeIntegrationInSubFrames: false`,
  `contextIsolation: true`, `sandbox: true`, `webSecurity: true`, no Blink or
  experimental features. Popups are *enabled* (`disablePopups: false`): a guest
  with them disabled drops `window.open` and `target=_blank` before the
  window-open handler is asked, so the link does nothing; enabled, the handler
  refuses every window and loads the page in the panel instead.
- `preload` → the panel preload, always (a guest that navigates keeps the
  preload it started with). The preload exposes its bridge **only** when
  `location.protocol === 'agentrq-ext:'`, and the main process checks the
  sender anyway.
- A `src` that is not `http(s):`, `agentrq-ext:` or `about:blank` is refused
  (`event.preventDefault()`).

### Asking before a page uses the camera, location and the rest

A page in the panel that asks for a permission (camera, microphone,
notifications, geolocation, MIDI, screen capture…) **asks the user**, the way a
browser does, instead of being silently allowed or refused:

- The panel session's `setPermissionRequestHandler` holds the request and sends
  it to the renderer, which shows a bar at the top of the panel, under the
  toolbar: *"example.com wants to use your camera"* with **Allow** and
  **Block**. The site named is the requesting frame's origin, and an Extension
  page is named by its Extension.
- The answer — Allow or Block — is **remembered for that origin and
  permission, on this machine only**: a JSON file per profile in the app's
  user-data folder (`side-panel-permissions/<profileId>.json`), written
  atomically, never sent to the server. Forgetting a profile deletes its file.
  `setPermissionCheckHandler` answers from the same record, so a page that only
  checks sees the same decision. A file that cannot be read is treated as
  empty (everything asks again), and a failed write is logged and keeps the
  decision for the run.
- **Site permissions** — the list that takes a decision back. Reached from the
  toolbar's *⋯* menu, and from a shield icon in the address field whenever the
  current site has a decision. It is drawn in the panel itself by the app (not
  a page in the guest, and not a new route), grouped by site: each row is the
  permission with *Allowed* or *Blocked* and a **Remove** button, and each site
  has *Remove all*. Removing a decision means the next request asks again.
- Navigating away, closing the panel, or a second request replacing the bar
  answers the pending one with Block. A request nobody answers within 60
  seconds is blocked — the main process never waits without a deadline.
- Unknown permission names are blocked without asking, so a new Chromium
  permission is not granted by default. `clipboard-sanitized-write` is allowed
  without asking, as browsers do.
- On macOS the operating system asks for camera and microphone access the first
  time as well; that dialog is the OS's and is not replaced.

Downloads go to the default Downloads folder with Electron's own prompt.

## Extensions

### Declaring a page

```json
"provides": {
  "panels": [
    { "id": "board", "label": "Board", "entry": "panel/index.html" }
  ]
}
```

Validated in `manifest.js` like `drawers`: `id` matches the existing id rule and
is unique, `label` is a non-empty string, `entry` is a relative `.html` path
inside the package. An invalid entry refuses the install with a reason, as a
bad drawer does.

### Serving it: `agentrq-ext://<extension-name>/<path>`

A privileged scheme (`standard`, `secure`, `supportFetchAPI`) registered on the
panel partitions only — the main window never loads it. Each Extension is its
own host and therefore its own origin, so its `localStorage` and IndexedDB are
its own and one Extension's page cannot read another's.

The handler serves a file only when:

1. the host names an installed, enabled Extension;
2. the path is inside the **directory of one of its declared panel entries**
   (`panel/` above) — not the whole package, which holds its Node code;
3. the resolved real path, symlinks followed, is still inside that directory —
   the same guard `readDrawer` applies, shared rather than copied.

Anything else is a 404 with a plain-text reason. MIME types come from the
existing table in `protocol.js`. No CSP is imposed: the page is code the user
chose to install, already running with more power in the main process than any
page could have.

### `ctx.panel` (`inject: ['panel']`)

```js
ctx.panel.open({ page: 'board' })                // one of its own pages
ctx.panel.open({ url: 'https://example.com' })   // any http(s) page
ctx.panel.onMessage(async (message) => reply)    // from its own page
ctx.panel.post(message)                          // to its own open page
```

- `open` asks the renderer to show the panel at that URL. A page id it did not
  declare, or a URL that is not `http(s):`, is a refusal with its name on it.
- `onMessage` registers one handler; registering again replaces it, and it is
  removed on unload like every other contribution.
- `post` delivers to the panel only when its current URL is that Extension's
  origin, and is otherwise dropped.
- Messages are JSON-serialisable values up to 1 MB. A handler that throws
  answers the page with an error carrying the Extension's name; a page whose
  Extension has no handler gets "This extension does not listen to its page."

### The page's side

```js
const reply = await window.agentrq.panel.send({ type: 'refresh' })
window.agentrq.panel.onMessage((message) => { … })
```

The main process routes `agentrq:panel:send` by **`event.senderFrame.url`**:
the scheme must be `agentrq-ext:` and the host is the Extension it reaches.
Nothing in the message chooses the recipient, so a web page cannot use the
bridge at all and one Extension's page can never reach another Extension.

### In the app

- The toolbar's *Pages* menu lists every installed Extension's declared panel
  pages, from the existing extensions state IPC.
- Uninstalling an Extension while its page is open: the next request is a 404
  and messages answer "That extension is no longer available."

## Telemetry

Two interface actions, reported through `recordUiAction` like the copy actions
(four places in the backend each, and the allowlist):

- `ui_side_panel_open` — the panel opened, from any route (dropped without a
  workspace in context, as every UI action is).
- `ui_side_panel_link` — a link from a task or message opened in the panel.

## Everything stays on this machine

The panel adds no server state. Width, open state and the last page are in
the renderer's `localStorage`; cookies and site storage are in the panel's own
partition; permission decisions are in the user-data file above. None of it is
synced, and signing in on another computer starts with an empty panel.

## Where things live

| | |
|---|---|
| `desktop/src/main/side-panel/guest.js` | `will-attach-webview` hardening, guest navigation/window-open rules (pure, tested) |
| `desktop/src/main/side-panel/ext-scheme.js` | the `agentrq-ext:` handler and its path guard |
| `desktop/src/main/side-panel/bridge.js` | routing page ↔ Extension by sender origin |
| `desktop/src/main/side-panel/permissions.js` | holding a permission request, the saved decisions, the deadline |
| `desktop/src/preload/panel.js` | the guest preload (built by `vite.preload.config.mjs`) |
| `desktop/src/main/extensions/manifest.js`, `host.js`, `runtime.js` | `provides.panels`, the `panel` registry |
| `frontend/src/components/SidePanel.vue` | toolbar, webview, resize handle, permission bar |
| `frontend/src/components/SidePanelPermissions.vue` | the site permissions list |
| `frontend/src/composables/useSidePanel.js` | open/width/URL state, persistence, clamping |
| `examples/extensions/panel-notes/` | an example: a page that lists workspace tasks via its Extension's MCP calls |

## Testing

- Desktop (vitest, coverage gate): manifest validation, guest hardening and
  URL rules, the scheme handler's path guard (traversal, symlink, undeclared
  directory, uninstalled Extension), bridge routing (wrong scheme, wrong host,
  no handler, throwing handler, size limit), permission requests (allow, block,
  saved and reloaded, unreadable file, failed write, removed, superseded, timed
  out, unknown name, check handler, profile forgotten), `ctx.panel`
  open/post/unload, the menu item.
- Frontend (vitest): `useSidePanel` clamping and persistence (storage
  throwing), `SidePanel.vue` toolbar, resize and permission bar, the site permissions
  list, `App.vue` link interception
  with and without modifiers, web build unaffected.
- Backend: the two actions through the telemetry allowlist and mapping tests.
- A headless Electron run of the real window (`offscreen: true`) loading a web
  page and an example Extension page, with screenshots in light and dark.

## Docs

`docs/EXTENSIONS.md` (a `panel` section, the manifest field, the example),
`docs/DESKTOP.md` (using the panel), `docs/agents/desktop.md` (the guest
boundary, why not `WebContentsView`, the drag trap), `desktop/README.md`.

## Not in this change

- Several tabs in the panel; one page at a time.
- Agents opening the panel through MCP.
- The panel in the web build.
