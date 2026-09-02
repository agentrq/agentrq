# AgentRQ Desktop

Electron shell around the AgentRQ web frontend. The renderer is built from
`../frontend/src` — the same `App.vue`, router, views and stores the browser
gets — so parity is structural rather than something to maintain by hand.

Both builds call the same `createAgentRQApp({ history, platform })` factory in
`frontend/src/app.js`, so there is exactly one route table. A view added to the
frontend appears here with no change on this side. `platform` is recorded in the
frontend's platform store, which is how a component offers a desktop-only
affordance without either build growing its own copy of the view.

Plan and phase breakdown: `docs/DESKTOP_APP_PLAN.md`. This package covers
phases 1-7: `0hua6QI7nXN`, `0hua7LpaQID`, `0hua8EL8awL`, `0hua8txMLIn`,
`0huaA6sKHoX`, `0huaAvwzIuH`, `0huaBmzhpvl`.

## Running it

```bash
cd frontend && npm install      # the renderer resolves its deps from here
cd ../desktop && npm install

npm run dev                     # Vite dev server + Electron, with HMR
npm run build                   # bundles renderer, main and preload into dist/
npm start                       # build, then run the packaged entry
npm run test:coverage           # unit tests, gated at 100% on main-process modules
npm run check:versions          # desktop, frontend and backend must agree
npm run package                 # real installers into release/, published nowhere
npm run package:dir             # unpacked app, much faster, for checking the bundle
```

On first run the app asks which AgentRQ server to connect to, defaulting to
the hosted instance at `https://app.agentrq.com`. The URL is probed before it is stored, so a typo is
caught there rather than surfacing later as a mysterious failure to sign in. The
choice lives in `agentrq-desktop.json` under Electron's userData directory, and
**Switch Server** in the application menu returns to that screen.

`AGENTRQ_SERVER_URL` pins the server for a launch and skips the connection
screen entirely — handy for pointing at a scratch backend without disturbing
stored settings:

```bash
AGENTRQ_SERVER_URL=http://localhost:3999 npm run dev
```

Google and GitHub sign-in work unchanged. `LoginView.vue` renders those as
ordinary links, which cannot work from the app:// origin, so the shell
intercepts the navigation and runs the flow in its own window against the real
server. That window shares the session, so the cookie it earns is the one the
proxy sends.

## How it works

The frontend addresses the API with same-origin *relative* URLs and
authenticates with the `at` cookie. A renderer loaded from `file://` would make
every one of those calls cross-origin, and the backend sends
`Access-Control-Allow-Origin: *` **without** `Allow-Credentials`, so the cookie
would never be attached.

So the renderer is served from a privileged `app://` scheme, and `src/main/protocol.js`
forwards everything under `/api`, `/mcp` and `/.well-known` to the configured
server using Electron's `net.fetch`. That runs against the session cookie jar,
so a login's `Set-Cookie` is stored against the real server host and replayed on
later calls. The renderer only ever sees same-origin traffic.

What this buys:

- `frontend/src/api.js` works **verbatim** — no desktop-specific branch
- cookie auth works, with the cookie held in the main process rather than the page
- `createWebHistory` routing works, because the handler does the same SPA
  fallback as the Go backend
- **no backend change** — web deployments are untouched

In dev the same handler proxies static requests to the Vite dev server instead of
reading from `dist/`, so dev and production run on an identical origin and HMR
still works.

## Verifying the architecture

Two scripts, both kept so the claims stay reproducible:

```bash
npm run spike:eventsource                    # is EventSource allowed on app://?
AGENTRQ_SERVER_URL=http://localhost:3999 \
AGENTRQ_ROOT_TOKEN=... npx electron scripts/verify-e2e.mjs
```

`verify:e2e` runs the real protocol handler against a real backend and checks
that the renderer mounts, the stylesheet is applied, an unauthenticated call is
refused, root-token login succeeds, the cookie is replayed, and the SSE stream
connects. `verify:connection` starts from nothing stored and drives the
first-run screen: a bad URL is refused with a reason and nothing is saved; a
good one is probed, stored, and reachable through the proxy.

Both assert a *computed* style rather than DOM presence alone — see the Tailwind
note below for why that check earns its place.

### Spike result: EventSource works over `app://`

This was the open risk in the plan (R2) — Chromium is stricter about
`EventSource` than about `fetch`, and `useEventBus.js` depends on it. With the
scheme registered as `standard` + `secure` + `stream` + `supportFetchAPI`,
EventSource connects and streams normally:

```
{ "verdict": "PASS", "eventSource": "open", "received": ["{\"tick\":1}", ...] }
```

**No preload SSE bridge is needed and `useEventBus.js` needs no platform
branch.**

## Profiles

Several accounts can be signed in at once, and switched between from the user
menu in the sidebar — the desktop equivalent of Chrome profiles.

A profile *is* an Electron session partition. That is the whole mechanism:
cookies are per partition, the `at` cookie is a cookie, so a partition is an
account. Everything else follows from keeping the list of them coherent.

Three consequences worth knowing before touching this:

- **The `app://` handler is registered per session, not once.** `protocol.handle`
  attaches to one session's registry, so a window opened on a partition that
  never had the handler cannot load the app at all. `sessionFor()` attaches it
  the first time a partition is used, and remembers which sessions already have
  it — registering twice throws.
- **Nothing may use `net.fetch` any more.** It sends the *default* session's
  cookies, so it would talk to the server as whichever account happened to be
  in that jar. Every request goes through `profileFetch`, which is the active
  profile's `session.fetch`. The same applies to the OAuth window: it runs on
  the active profile's partition, so the cookie the callback sets signs in that
  profile and no other.
- **Switching replaces the window rather than reloading it.** A window's
  partition is fixed when it is created. Reloading would keep the old session
  and quietly show the previous account's data under the new profile's name.
  The replacement window is created *before* the old one is destroyed: on
  Windows and Linux, closing the last window quits the app.

Each profile carries its own server URL and muted-workspace list, because an
account exists on one server — splitting those would let you sign in as one user
and then point that session at a server where the cookie means nothing.

Adding a profile switches into a session that has never signed in, so the window
lands on the connection screen. That screen therefore has to offer a way back:
`connectionState()` reports `canCancel`, which is
`canDiscardActiveProfile(profileState)` — more than one profile, and the active
one has no server yet. A first run answers false, because there is nowhere to go
and the app cannot start without a server. Cancelling *discards* the profile
rather than leaving it behind: it points at no server, so it can do nothing, and
the switcher offers no way to delete one. It returns to the profile the add was
made from, which is why `removeProfile` takes a fallback id — with three or more
profiles, the first in the list is not where the user came from.

The profile migrated from a pre-profiles install gets a partition like any
other, which signs that user out once on upgrade. That was a deliberate trade:
the session is only valid for 24 hours anyway, and the alternative is one
profile permanently special-cased wherever a session is resolved.

## The native shell

Everything the browser cannot offer lives in the main process and reaches the
app through one navigation channel, so the shared Vue app needs no knowledge of
any of it:

- **Application menu** with the platform roles plus New Task, Switch Server, Log
  Out and Check for Updates. The last is present but disabled until phase 6
  supplies an updater — a menu item that silently does nothing is worse than one
  that is visibly not ready.
- **Tray item** carrying the unread count and the workspaces that have been
  active most recently, ordered by activity rather than alphabetically: it is a
  shortcut to what is happening now, not a second sidebar.
- **Global shortcut** `Cmd/Ctrl+Shift+N` for a new task. `Shift` keeps it clear
  of the browser-standard new-window binding, which matters for a shortcut
  registered system-wide. Registration failing because another app owns the
  combination is the user's environment, not an error worth interrupting them
  over — the same action stays on the menu and the tray.
- **`agentrq://` deep links**, e.g. `agentrq://workspaces/<id>/tasks/<id>`.
  These are untrusted input from outside the app, so a link is checked against
  the routes that actually exist rather than passed to the router; an
  unrecognised one opens the default view. macOS delivers them as an event,
  Windows and Linux as an argument to a second launch, which the single-instance
  lock forwards rather than opening a rival window.
- **Window state** across restarts, clamped to a display that still exists.
  Restoring saved coordinates blindly is how a window ends up off-screen after a
  monitor is unplugged — visible to no one, draggable by no one.
- **Theme** follows the app's own `themeStore`, never `prefers-color-scheme`. A
  user who chose light inside AgentRQ should not get a dark title bar because
  their system is dark. The window background is set from it too, which is what
  stops a white flash on reload in dark mode.

The tray icon is deliberately an empty image for now: it is an art asset, and
shipping a wrong-looking one is worse than the platform default. The menu, count
and tooltip — the parts carrying information — are real.

## Releasing

Tagging `v<version>` runs `.github/workflows/desktop-release.yml`, which builds
on all three platforms and publishes to a GitHub Release:

| Runner | Targets |
|---|---|
| macos-latest | dmg + zip, arm64 and x64 |
| windows-latest | NSIS, x64 and arm64 |
| ubuntu-latest | AppImage + deb, x64 and arm64 |

That Release is also the update feed, so the `latest*.yml` manifests published
beside the installers matter as much as the installers do.

Two things guard it. Versions are checked first, in a job that costs seconds and
fails before any twenty-minute build starts: the desktop, frontend and backend
versions must agree, and the tag must match them — `v0.5.0` cannot be cut from a
tree stamped 0.4.8, which would name a release for one version and fill it with
artifacts for another. And the release is assembled as a **draft**, published
only once every platform has finished, so nobody ever sees a release missing the
installer for their machine and no updater reads a half-populated feed.

Pull requests touching `desktop/` or `frontend/` run the same build without
publishing, because a packaging break is invisible to the ordinary test job —
that one never runs electron-builder at all.

### Signing

Signing is credential-gated rather than assumed. With no certificate configured
the build still succeeds and ships unsigned:

- **Windows and Linux** install and update normally; Windows shows a SmartScreen
  warning on first install.
- **macOS cannot auto-update at all.** Squirrel.Mac validates the signature
  before installing, so an unsigned macOS build can be downloaded and run but
  never updates itself. The app reports this as *"This build is not signed, so
  it cannot update itself"* rather than a raw error.

Adding these secrets is all that is needed to turn signing on:
`MAC_CERTIFICATE`, `MAC_CERTIFICATE_PASSWORD`, `APPLE_ID`,
`APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`, and optionally
`WIN_CERTIFICATE` / `WIN_CERTIFICATE_PASSWORD`.

Until those exist, the supported way for a macOS user to move between versions
is the install script, which lives in the `agentrq-static` repository at
`src/install.sh` and is published as <https://agentrq.com/install.sh>:

```sh
curl -fsSL https://agentrq.com/install.sh | sh
```

It replaces the bundle wholesale rather than asking Squirrel.Mac to patch it, so
the signature check never enters into it, and it clears the quarantine attribute
so Gatekeeper does not challenge the result. That matters more than it used to:
macOS 15 removed the right-click -> Open bypass, so an unsigned app downloaded
by hand now needs a trip through System Settings.

### Deep links have to be declared at packaging time

`app.setAsDefaultProtocolClient()` at runtime is enough for Windows and Linux,
but macOS only routes a scheme to an app that declares it in its bundle. The
`protocols` entry in `electron-builder.yml` is what puts `CFBundleURLTypes` in
`Info.plist`; without it `agentrq://` links silently do nothing on a packaged
macOS build, and no amount of runtime code fixes that.

## Auto-update

Updates come from this repository's GitHub Releases, published by the workflow
in phase 7. The app checks at launch and every six hours, downloads in the
background, and installs on quit if the user never acts on the prompt.

The prompt itself is **App.vue's existing "a new version is available" banner** —
the one the browser build shows when a service worker is waiting. The desktop
build resolves `virtual:pwa-register/vue` to
`frontend/src/desktop/useDesktopUpdates.js`, which presents the Electron updater
through the same interface App.vue already consumes. So the desktop update
prompt is pixel-identical to the web one by construction, App.vue knows about
neither, and the design system's ban on native modals is satisfied without
inventing anything.

Transient states go through the toast system instead: checking, up to date, and
failures. A background check that finds nothing stays **silent** — six-hourly
"you are up to date" toasts would be pure noise — while the same answer to a
question asked from the menu is reported, because someone is waiting for it.

**Updates are hard-disabled when the app is unpackaged**, so `make dev` never
reaches the update path. Asking from the menu in a development build says so
rather than appearing broken.

### macOS needs a signed build

Squirrel.Mac validates the code signature before installing, so an unsigned
macOS build cannot update itself at all. That is a phase 7 concern (signing
certificates, still an open question in the plan); the failure is recognised and
reported here as *"This build is not signed, so it cannot update itself"* rather
than as a raw error nobody could act on. Windows and Linux are unaffected.

The escape hatch is `https://agentrq.com/install.sh` -- see
[Signing](#signing). It is worth wiring that command into this message so a
user does not have to go looking for it.

## Notifications

Web Push cannot work in Electron - there is no push service behind it - so the
desktop app produces the same user-facing behaviour from the event stream it
already has. The main process holds its own SSE connection (separate from the
renderer's, because notifications must keep arriving while the window is in the
background), maps events to native notifications, and drives the dock badge or
Windows taskbar overlay. Clicking one focuses the window and routes to the task.

Wording, trigger rules and click destinations mirror
`backend/internal/controller/push/push.go`, so a user moving between the browser
and the desktop app sees the same notifications for the same events. Only agent
activity notifies - being told about your own click is noise. Reconnect and
backoff match `useEventBus.js`: one second, doubling to thirty, and a hard stop
on 401.

`frontend/src/composables/usePushNotifications.js` is untouched and still serves
the browser build; the desktop renderer simply never calls it.

Per-workspace muting lives in the workspace settings, beside the browser's push
toggle, and is stored locally in the same config file as the server URL.

### The backend publishes each task event twice

One task creation reaches the stream twice: the REST handler publishes
`task.created` directly, and the CRUD-event consumer publishes it again from the
same write. The renderer never noticed - it just re-renders - but two identical
native notifications for one event is plainly wrong. Confirmed against a live
backend: two events in, one notification out.

Notifications are therefore collapsed by `tag` within a short window. That is the
same field, carrying the same value, that the browser uses to collapse duplicate
web-push notifications; this applies the mechanism on our side rather than
changing the backend's publishing.

### Tailwind scans from the build root, which is not frontend/

Tailwind v4 decides which files to scan by walking out from the build's root.
The desktop build's Vite root is `src/renderer`, so the scan found only the body
classes in that `index.html` and dropped every utility the actual views use — a
13 KB stylesheet where the web build produces 102 KB. The DOM looked correct
throughout; the app simply rendered unstyled.

`frontend/src/style.css` now declares `@source './'`, which resolves to
`frontend/src` in both builds and needs no knowledge of either. It also holds in
the production Docker image, which copies only `./frontend` — an `@source`
pointing at `desktop/` would not exist there.

The end-to-end checks assert a computed `border-radius`, so this cannot regress
silently again.

### Backend issue found while verifying (since fixed)

`eventsHandler` in `backend/internal/app/app.go` sets the SSE headers but never
flushes them, so Go buffers the response until the first real event or the 30s
keepalive tick. `EventSource.onopen` therefore fires up to 30 seconds late.

This was **not** desktop-specific — the web app's connection indicator had the
same delay. Confirmed with plain `curl` against the backend, with no Electron
involved. Fixed in #361 (task `0huclLMe3BB`) by flushing the headers on connect;
the end-to-end check now sees the stream open immediately rather than after 30
seconds.

## Layout

```
src/renderer/main.js        bootstrap — connection screen, or createAgentRQApp
src/main/protocol.js        app:// handler and API proxy — the core of the design
src/main/server-config.js   which server to talk to: normalisation, storage, probing
src/main/auth.js            OAuth sign-in taken over from the login view's links
src/main/menu.js            application menu (switch server, log out)
src/main/sse.js             event-stream client for the main process
src/main/notifications.js   events -> native notifications, mute rules, badge
src/main/deep-link.js       agentrq:// links -> in-app routes
src/main/tray.js            tray menu, recent workspaces
src/main/theme.js           native chrome follows the app's theme store
src/main/window-state.js    remembering where the window was, safely
src/main/updater.js         auto-update state machine over electron-updater
src/main/index.js           lifecycle, window creation, scheme registration, IPC
src/preload/index.js        the narrow contextBridge surface
scripts/                    dev launcher, build, spike, e2e verification
test/                       unit tests (plain Node — no Electron binary needed)
```

The connection screen itself lives at `frontend/src/desktop/ConnectionView.vue`,
not here — see the note at the top of that file for why moving it into this
package would silently strip its styling.

Electron APIs are injected into the main-process modules rather than imported,
which is what lets the routing rules be tested without launching Electron.
