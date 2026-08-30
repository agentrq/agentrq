# AgentRQ Desktop

Electron shell around the AgentRQ web frontend. The renderer is built from
`../frontend/src` — the same `App.vue`, router, views and stores the browser
gets — so parity is structural rather than something to maintain by hand.

Both builds call the same `createAgentRQApp({ history, platform })` factory in
`frontend/src/app.js`, so there is exactly one route table. A view added to the
frontend appears here with no change on this side. `platform` is recorded in the
frontend's platform store, which is how a component offers a desktop-only
affordance without either build growing its own copy of the view.

Plan and phase breakdown: `docs/DESKTOP_APP_PLAN.md`. This package covers phase 1
(TaskID `0hua6QI7nXN`) and phase 2 (`0hua7LpaQID`).

## Running it

```bash
cd frontend && npm install      # the renderer resolves its deps from here
cd ../desktop && npm install

npm run dev                     # Vite dev server + Electron, with HMR
npm run build                   # bundles renderer, main and preload into dist/
npm start                       # build, then run the packaged entry
npm run test:coverage           # unit tests, gated at 100% on main-process modules
```

By default the app talks to `http://localhost:3000`. Point it elsewhere with
`AGENTRQ_SERVER_URL`:

```bash
AGENTRQ_SERVER_URL=http://localhost:3999 npm run dev
```

The first-run connection screen that replaces this environment variable is
phase 3 (`0hua8EL8awL`).

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

`verify-e2e.mjs` runs the real protocol handler against a real backend and
checks that the renderer mounts, an unauthenticated call is refused, root-token
login succeeds, the cookie is replayed, and the SSE stream connects.

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

### Known backend issue found while verifying

`eventsHandler` in `backend/internal/app/app.go` sets the SSE headers but never
flushes them, so Go buffers the response until the first real event or the 30s
keepalive tick. `EventSource.onopen` therefore fires up to 30 seconds late.

This is **not** desktop-specific — the web app's connection indicator has the
same delay today. Confirmed with plain `curl` against the backend, with no
Electron involved. Tracked separately as `0huclLMe3BB`; a single `flusher.Flush()`
after the headers fixes it.

## Layout

```
src/renderer/main.js        bootstrap — calls the frontend's createAgentRQApp
src/main/protocol.js        app:// handler and API proxy — the core of the design
src/main/server-config.js   which server to talk to, and URL normalisation
src/main/index.js           app lifecycle, window creation, scheme registration
src/preload/index.js        the narrow contextBridge surface
src/renderer/               entry point and the vite-plugin-pwa stub
scripts/                    dev launcher, build, spike, e2e verification
test/                       unit tests (plain Node — no Electron binary needed)
```

Electron APIs are injected into the main-process modules rather than imported,
which is what lets the routing rules be tested without launching Electron.
