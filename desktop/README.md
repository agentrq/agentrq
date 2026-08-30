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
phase 1 (`0hua6QI7nXN`), phase 2 (`0hua7LpaQID`) and phase 3 (`0hua8EL8awL`).

## Running it

```bash
cd frontend && npm install      # the renderer resolves its deps from here
cd ../desktop && npm install

npm run dev                     # Vite dev server + Electron, with HMR
npm run build                   # bundles renderer, main and preload into dist/
npm start                       # build, then run the packaged entry
npm run test:coverage           # unit tests, gated at 100% on main-process modules
```

On first run the app asks which AgentRQ server to connect to, defaulting to
`http://localhost:3000`. The URL is probed before it is stored, so a typo is
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
