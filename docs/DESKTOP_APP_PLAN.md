# AgentRQ Desktop (Electron) — Implementation Plan

TaskID: 0huZWzzheT3

## Goal

Ship an Electron desktop application with **exact functional and visual parity** with the
existing web frontend, built by **re-using the same Vue components** rather than
re-implementing them, plus in-app auto-update. The desktop app is a superset: everything the
web frontend supports it supports, and over time it adds capabilities the browser cannot
offer (native notifications, tray, global shortcuts, deep links, local filesystem access).

Parity is not a one-time port. The design below makes the desktop app consume the frontend's
component tree and route table directly, so a new view added to `frontend/src` appears in the
desktop app with no extra work.

---

## 1. What we are building on

| Concern | How the web frontend does it today | Consequence for Electron |
|---|---|---|
| App shell | Vue 3 + Vite + Tailwind v4, Pinia, `vue-router` with `createWebHistory` | Reusable as-is if the desktop origin serves an SPA fallback |
| Serving | Go backend serves `./public` and injects `window.__AGENTRQ_BASE_PATH__` | Desktop must supply its own equivalent of that injection |
| API access | Same-origin **relative** URLs — `${basePath}/api/v1` (`src/api.js`) | Desktop must make the renderer believe it is same-origin, or every call breaks |
| Auth | `at` JWT **cookie**, set by the backend on login | Cross-origin cookies would be blocked (see risk R1) |
| Live updates | **SSE** via `EventSource` on relative URLs (`src/useEventBus.js`) | Streaming must survive whatever transport we choose |
| Notifications | Web Push — service worker + browser push service (`usePushNotifications.js`) | **No Electron equivalent**; needs a different mechanism |
| Offline | `vite-plugin-pwa` service worker | Disabled in the desktop build; the bundle is already local |
| Speech-to-text | `@huggingface/transformers` in a web worker | Works, but needs CSP + WASM allowances and fetches models from the HF CDN |

---

## 2. Architecture

### 2.1 Repository layout

A new top-level `desktop/` package. `frontend/` stays the single source of truth for all UI.

```
desktop/
  package.json               electron, electron-builder, electron-updater, vite, vitest
  electron-builder.yml       packaging + publish targets
  vite.main.config.mjs       main-process bundle
  vite.preload.config.mjs    preload bundle
  vite.renderer.config.mjs   renderer build — aliases @app -> ../frontend/src
  src/main/
    index.js                 app lifecycle, window creation, single-instance lock
    protocol.js              app:// scheme handler + API proxy   <- the core of the design
    server-config.js         configured server URL, persisted in userData
    auth.js                  OAuth window flow, shared cookie jar
    notifications.js         SSE -> native OS notification
    updater.js               electron-updater wiring
    menu.js / tray.js / shortcuts.js / window-state.js
  src/preload/index.js       contextBridge -> window.agentrq
  src/renderer/
    index.html
    main.js                  thin bootstrap that calls the shared app factory
  resources/icons/
```

### 2.2 The core idea: a same-origin illusion

The single hardest constraint is that `src/api.js` and `useEventBus.js` use **relative**
URLs against a **cookie**-authenticated backend. A naive Electron app loads the renderer from
`file://`, making every API call cross-origin — and the backend's CORS is `AllowOrigins: "*"`
with no `AllowCredentials`, so the browser would refuse to attach the `at` cookie. Loosening
backend CORS to accept credentials from a desktop origin is possible but widens the server's
attack surface for every deployment, including hosted ones.

Instead, we register a **privileged custom scheme** and proxy through the main process:

```js
// main — registered before app.whenReady()
protocol.registerSchemesAsPrivileged([{
  scheme: 'app',
  privileges: { standard: true, secure: true, supportFetchAPI: true, stream: true, corsEnabled: true },
}])

// main — after ready
protocol.handle('app', async (request) => {
  const { pathname } = new URL(request.url)

  // API / MCP traffic is forwarded to the configured AgentRQ server.
  // net.fetch uses the Electron session cookie jar, so Set-Cookie from the
  // backend is stored against the real server host and replayed automatically.
  if (pathname.startsWith('/api/') || pathname.startsWith('/mcp') || pathname.startsWith('/.well-known/')) {
    return net.fetch(new URL(pathname + search, serverURL).toString(), {
      method: request.method,
      headers: request.headers,
      body: request.body,
      duplex: 'half',
      credentials: 'include',
    })
  }

  // Everything else: bundled static asset, with SPA fallback to index.html —
  // mirroring the Go backend's own fallback rule.
  return serveStatic(pathname)
})
```

The window loads `app://agentrq/`. From the renderer's point of view every request is
same-origin, so:

- `src/api.js` works **verbatim** — no edits.
- Cookie auth works, because the cookie lives in the main process session jar, not in the
  renderer's origin.
- `createWebHistory` routing works, because the handler does SPA fallback.
- **No backend change is required.** Web deployments are untouched.

`window.__AGENTRQ_BASE_PATH__` is set to `''` in the desktop `index.html`, matching a
root-mounted deployment.

### 2.3 Sharing the component tree (parity guarantee)

`frontend/src/main.js` today hard-codes the route table alongside its bootstrap. Duplicating
that table in the desktop renderer would let the two drift — exactly the parity failure we
want to design out. So we make one small, behaviour-preserving refactor to the frontend:

- Extract the routes and app wiring into `frontend/src/app.js`, exporting
  `createAgentRQApp({ history, platform })`.
- `frontend/src/main.js` becomes a three-line web bootstrap calling it with `createWebHistory`.
- `desktop/src/renderer/main.js` calls the same factory with `platform: 'desktop'`.

One route table, one `App.vue`, one set of views and stores. A new view added to the frontend
is automatically present in the desktop app.

The `platform` flag is the seam for desktop-only behaviour: it lets a component render a
native affordance (e.g. an "Open in Finder" action) without forking the component.

### 2.4 Connecting to a server

The desktop app is a client; the AgentRQ server (self-hosted or hosted) stays where it is.

- **First run**: a connection screen asks for the server URL, defaulting to
  `http://localhost:3000`. It is validated by calling `/api/v1/auth/config` before being saved
  to `userData`.
- **Login**: the existing `LoginView.vue` renders unchanged. Root-token login works through
  the proxy immediately. Google/GitHub OAuth redirects must happen against the **real** https
  origin, so they open in a dedicated `BrowserWindow` sharing the same session partition; when
  the flow completes, the `at` cookie lands in the shared jar and the main window reloads.
- **Switch server / log out** lives in the application menu and clears the session jar.

### 2.5 Notifications

Web Push cannot work in Electron — there is no push service behind it. The desktop app
achieves the same user-facing behaviour over the transport we already have:

- The main process holds a persistent SSE connection to `/api/v1/events/stream`.
- Relevant events raise a native `Notification`.
- Clicking one focuses the window and routes to the task, via the preload bridge.
- Unread count drives the macOS dock badge / Windows overlay icon.
- Per-workspace mute settings are stored locally.

`usePushNotifications.js` stays exactly as it is for the browser build; the desktop renderer
simply does not use it.

### 2.6 Auto-update

`electron-updater` against the **GitHub Releases** provider on this repository.

- Check on launch, then every 6 hours; download in the background.
- When ready, surface a prompt through the existing `useToasts` system — consistent with the
  design system's "no native modals" rule — offering restart-now or on-next-quit.
- Disabled when running unpackaged, so `make dev` never hits the update path.
- The desktop version is kept in lockstep with the repo version (currently `0.4.8`, tracked in
  `frontend/package.json` and `backend/internal/service/config/config.go`); the release
  workflow asserts they match rather than letting them drift.

**Signing is a hard prerequisite on macOS**: Squirrel.Mac validates the code signature, so an
unsigned macOS build **cannot auto-update at all**. Windows NSIS updates function unsigned but
trigger SmartScreen warnings. See open question Q1.

### 2.7 Build and release pipeline

`electron-builder` with a new `.github/workflows/desktop-release.yml`, triggered on `v*` tags
(alongside the existing `docker-release.yml`), matrixed over:

| Runner | Targets |
|---|---|
| `macos-latest` | dmg + zip, arm64 and x64 |
| `windows-latest` | nsis, x64 |
| `ubuntu-latest` | AppImage + deb, x64 |

Artifacts and the `latest*.yml` update manifests are published to the GitHub Release, which is
the auto-update feed. A `pull_request` job builds without publishing so packaging breaks are
caught in review.

### 2.8 Testing

The frontend currently has no test runner. The desktop package introduces **Vitest** with c8
coverage, and the main-process modules are written to be unit-testable in isolation:

- `protocol.js` — route classification, SPA fallback, header/body forwarding, streaming
- `server-config.js` — URL normalisation, validation, persistence, migration
- `updater.js` — state machine, dev-mode guard
- `notifications.js` — event → notification mapping, mute rules, badge counting
- `auth.js` — OAuth callback matching, session clearing

Electron APIs are injected rather than imported directly, so tests run in plain Node with no
Electron binary. Every PR reports 100% coverage on new lines, per the project's PR rules; a CI
job enforces the threshold.

---

## 3. Delivery phases

Each phase is one PR with its own tests and coverage report.

| # | Phase | TaskID | Outcome |
|---|---|---|---|
| 1 | Scaffold + protocol spike | `0hua6QI7nXN` | `desktop/` package, `app://` handler with API proxy, SSE transport validated, Vitest + coverage gate. A window that loads the real Vue app against a local backend and logs in with a root token. |
| 2 | Shared app factory | `0hua7LpaQID` | `createAgentRQApp` extracted; web and desktop share one route table. No behaviour change to the web build. |
| 3 | Server connection + auth | `0hua8EL8awL` | First-run connection screen, validation, OAuth window flow, switch-server / logout. |
| 4 | Native notifications | `0hua8txMLIn` | SSE → native notifications, click-to-route, badges, per-workspace mute. |
| 5 | Native shell | `0huaA6sKHoX` | Application menu, tray, global quick-create shortcut, `agentrq://` deep links, window-state persistence, dark mode wired to the existing `.dark` class store. |
| 6 | Auto-update | `0huaAvwzIuH` | `electron-updater` wiring and update UI. |
| 7 | Release pipeline | `0huaBmzhpvl` | `electron-builder` config, GH Actions matrix, signing/notarization, version-sync check. |
| 8 | Docs | `0huaCOrjhtR` | `docs/DESKTOP.md`, README section, `make desktop` / `make desktop-dev` targets. |

Phases 1–2 are sequential; 3–6 can proceed in parallel once 2 lands; 7 depends on 6; 8 is last.

---

## 4. Risks

**R1 — Cross-origin cookie auth (mitigated by design).** Backend CORS is `AllowOrigins: "*"`
with no `AllowCredentials`, so a conventional cross-origin renderer could not authenticate.
The main-process proxy sidesteps this entirely and needs no backend change.

**R2 — `EventSource` under a custom scheme. RESOLVED in phase 1: it works.** The concern was
that Chromium might restrict `EventSource` to http/https even for a privileged scheme, which
would have forced a preload SSE bridge and a platform branch inside `useEventBus.js`. With the
scheme registered as `standard` + `secure` + `stream` + `supportFetchAPI`, EventSource
connects and streams normally — verified both in isolation
(`desktop/scripts/spike-eventsource.mjs`) and against a real backend through the proxy
(`desktop/scripts/verify-e2e.mjs`). No bridge is needed and `useEventBus.js` is unchanged.

**R2b — the backend never flushes its SSE headers (found while verifying R2).** Go buffers the
response until the first event or the 30-second keepalive, so `EventSource.onopen` fires up to
30 seconds late. This affects the **web** frontend today, not just the desktop app: the
connection indicator reads disconnected on a healthy stream. Reproduced with plain curl.
Tracked as `0huclLMe3BB`; the fix is a single `flusher.Flush()` after the headers.

**R3 — macOS auto-update requires signing.** Without a Developer ID certificate and
notarization, macOS auto-update simply does not function. Blocking for phase 7 (Q1).

**R4 — Transformers.js / WASM under a custom scheme.** The speech-to-text worker needs CSP and
cross-origin-isolation headers that our protocol handler must set explicitly; model downloads
from the HF CDN must be allowed through.

**R5 — Bundle size.** Electron adds ~90 MB per platform artifact before our code. Expected,
but worth stating so release-asset growth is not a surprise.

**R6 — Design-system drift.** Because the desktop app renders the same components, drift can
only come from desktop-only chrome (title bar, menus). Those follow `frontend/AGENTS.md` — flat
surfaces, `rounded-lg` actions, `useToasts` for feedback, never a native `confirm()`.

---

## 5. Open questions

**Q1 — Code signing.** Do we have (or want to buy) an Apple Developer ID certificate plus
notarization credentials, and a Windows signing certificate? macOS auto-update is impossible
without the former. If neither is available yet, phases 1–6 still land and phase 7 ships
unsigned Linux/Windows builds with macOS auto-update deferred.

**Q2 — Server connection model.** The plan assumes the desktop app **connects to an AgentRQ
server** the user points it at. The alternative — bundling the Go backend inside the app for a
fully self-contained offline instance — is a materially larger scope (per-platform native
binaries, data-directory migration, signing a bundled executable) and would be a separate
follow-on. Confirm the client model is what you want for v1.

**Q3 — Distribution channel.** GitHub Releases as the update feed (assumed), or an eventual
Mac App Store / Microsoft Store presence? Store distribution forbids self-updating and would
change phase 6 substantially.

**Q4 — First desktop-only capability.** The plan builds the seam (`platform: 'desktop'`) for
capabilities the browser cannot offer. Knowing the first one you actually want would let us
validate the seam in phase 5 rather than after.
