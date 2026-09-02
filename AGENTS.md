# AgentRQ Codebase Notes

## Project layout

- `backend/` — Go backend (Fiber HTTP, GORM, MCP server)
- `frontend/` — Vue3 frontend, and the single source of UI for **both** the web and desktop builds
- `desktop/` — Electron shell (see below); its renderer is built from `frontend/src`
- `plugins/` — harness plugins published from this repo (`plugins/deepseek-harness` → the `@agentrq/dsh-plugin-agentrq` bundle for DeepSeek Harness)

## Desktop app (`desktop/`)

The desktop app renders the *same* Vue application as the browser. Both call
`createAgentRQApp({ history, platform })` from `frontend/src/app.js`, so there is
exactly one route table — **never add a route anywhere else**, or the two builds
drift apart silently.

### The app:// proxy — do not reintroduce cross-origin API calls

The frontend addresses the API with same-origin **relative** URLs (`src/api.js`)
and authenticates with the `at` cookie. Backend CORS is `AllowOrigins: "*"` with
no `AllowCredentials`, so a renderer on its own origin could never attach that
cookie.

The desktop renderer is therefore served from a privileged `app://` scheme, and
`desktop/src/main/protocol.js` forwards `/api`, `/mcp` and `/.well-known` to the
configured server from the main process, where Electron's session cookie jar
holds the credentials. **The renderer only ever sees same-origin traffic.**

Consequences worth knowing before changing anything here:

- **Never introduce an absolute API URL in frontend code.** It would work in the
  browser and break the desktop app, where it becomes a cross-origin request
  with no cookie.
- **Never relax backend CORS to accommodate the desktop app.** It does not need
  it, and doing so widens the attack surface of every deployment.
- Desktop-only capabilities reach the renderer through the narrow `window.agentrq`
  bridge in `desktop/src/preload/`. Components branch on `usePlatformStore()`,
  never on user-agent sniffing or probing for `window.agentrq`.

### Three traps that are invisible in source

- **Tailwind scans from the build root.** The desktop build's Vite root is
  `desktop/src/renderer`, so a class used only in a file under `desktop/` is
  silently dropped from the stylesheet — the DOM looks right and the app renders
  unstyled. `frontend/src/style.css` declares `@source './'` to fix this, and
  desktop-only *views* live in `frontend/src/desktop/` for the same reason.
  Moving them into `desktop/` breaks their styling with no error.
- **macOS hides the title bar, so the page owns the window.** The window is
  created with `titleBarStyle: 'hiddenInset'`, and a window with no title bar
  cannot be dragged until the page declares a region with `-webkit-app-region:
  drag` — the `.app-drag` class. The traffic lights are also drawn over the
  top-left of the page, so that same strip reserves their space. Both are gated
  on `platformStore.isMacDesktop`; making page content draggable on Windows or
  Linux would only remove text selection.
- **macOS only routes a URL scheme an app declares in its bundle.** Calling
  `app.setAsDefaultProtocolClient()` is enough for Windows and Linux, but the
  `protocols` entry in `desktop/electron-builder.yml` is what makes
  `agentrq://` links work on a packaged macOS build.

Full detail, including the verification scripts, is in `desktop/README.md`.
User-facing documentation is `docs/DESKTOP.md`.

## Running tests

```bash
cd backend && go test ./internal/...
```

Mock packages are **generated** (gitignored). Run `make mocks` before testing if they are missing. `mockgen` lives at `~/go/bin/mockgen`.

## MCP server (`backend/internal/controller/mcp/`)

- `server.go` — all tool handlers (`handleCreateTask`, `handleReply`, etc.) and the `WorkspaceServer` struct
- Cron validation: `validateCronGranularity` enforces hourly-minimum granularity. Minute field must be a single fixed integer (0-59); wildcards/steps/ranges/comma-lists are rejected.
- Creating a task with `cron_schedule` sets `status="cron"` on the model.

## CRUD task controller (`backend/internal/controller/crud/task.go`)

- Cron validation also lives here for the REST API path (same rules).
- `isValidTaskStatus` — valid statuses: `notstarted`, `ongoing`, `completed`, `rejected`, `cron`, `blocked`.

## Events (experimental)

Named signals that let one workspace trigger tasks in another.

- **DB models**: `events` and `event_triggers` tables (monoflake IDs).
- **REST API** (all under `/api/v1/events`): CRUD for events + triggers, plus `GET /events/:id/tasks` to list tasks spawned by an event. These routes are handled by Fiber — do NOT add them to the stdlib `mux` in `app.go` (that mux is only for SSE and pub-stats routes).
- **MCP tool**: `publishEvent` — agents call this to fire an event by name with a payload and optional FAQ.
- **Task `eventId` field** (base62): set at task creation (REST or MCP) to link a task to an event. Publishing is **agent-driven**: the createTask notification sent to the agent via the MCP channel appends `[On completion: call publishEvent("name", "...")]` so the agent publishes the linked event with a meaningful payload before completing. (There is no automatic publish on completion; completing a task does not by itself fire the event.)
- **Consumer**: `backend/internal/controller/event/` subscribes to `PubSubTopicEvents` (ID 3) and fans out to all `EventTrigger` rows, creating tasks via `renderTemplate` substituting `{{EVENT_PAYLOAD}}` and `{{EVENT_FAQ}}` **in the body only** — the title is always static text.
- **`EventTrigger.emitEventId`**: optional field that chains events — when the trigger's spawned task completes it publishes this second event. The consumer appends the same `publishEvent` instruction to the task body. Triggered tasks always start as `notstarted` (no cron scheduling).
- **Frontend**: `/events` list + `/events/:id` detail (triggers CRUD + resulting tasks, 10 shown with load-more). Both the task-creation form and the trigger-creation form have an optional "Emit event on completion" selector.

## Commit convention

Include `Task: <taskID>` in the commit body for traceability.

## Coding Standards

- **API Naming**: All JSON fields in API requests and responses MUST use `camelCase` (e.g., `workspaceId`, `createdAt`). Never use `snake_case` in the API surface.
- **Backend Layers**: Follow view-entity-model separation; only `view` structs define the API schema. Avoid using repository directly from handlers; use controller methods instead.
