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
- **Never make a `file:` URL followable.** `classifyLink` blocks the scheme on
  purpose — message bodies are agent-written, and a followed `file:` link is how
  one reaches the machine. Rendered markdown therefore strips the href and
  parks the URL in `data-file-url` (`frontend/src/utils/markdown.js`); clicking
  it is a bridge request answered by `desktop/src/main/files.js`, which opens
  only file types that are read rather than run and reveals everything else in
  the file manager.
- **Copying from the renderer goes through the shell on desktop.**
  `navigator.clipboard.writeText` throws in a window that is not frontmost, so
  `writeClipboard` prefers `window.agentrq.clipboard` and falls back to the
  browser API only where there is no shell.

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

### Workspace memory

`loadMemory` / `saveMemory` (`memory.go`) give agents notes that outlive a task.

- Stored in `memories`, keyed per **(workspace owner, workspace, name)** — it is
  the *workspace's* memory, so every agent working there shares it. Keying it to
  the connecting client would make it per-agent memory wearing a workspace's
  name.
- `memory.md` is the default for both tools and is meant to stay an index of the
  other named memories, linking them as `memory://<name>`. The server
  instructions tell connecting agents to load it first.
- **Names are canonicalised at the tool boundary**: trimmed, lowercased, and
  then required to match `^[a-z0-9]+(-[a-z0-9]+)*\.md$`. So `MEMORY.md`,
  `Memory.md` and `memory.md` are one memory rather than three. Folding here
  rather than in a query is deliberate — `=` is case-sensitive by default on
  both SQLite and Postgres and a `NOCASE` collation does not port between them,
  so one stored spelling is what makes the unique key behave the same on either.
  The strict shape is also what lets `memory://<name>` be parsed as a URL at
  all: a name with a space in it is not one.
- **Limits are enforced at the tool boundary, not in the repository**: 16 KiB of
  UTF-8 per memory and 32 characters per name, both refused rather than
  truncated — an agent told its memory is too large can split it, while one
  silently cut in half cannot know to.
- A `loadMemory` miss is **not** an error: every agent's first call on a fresh
  workspace misses, and answering with an error teaches agents to stop asking.

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

## WebMCP (`frontend/src/webmcp/`)

The interface offers itself to a browser agent as WebMCP tools — 52 of them,
registered on sign-in and withdrawn on sign-out.

- `modelContext.js` is the browser seam (finds `document.modelContext`, falling
  back to the deprecated `navigator.modelContext`); `tools.js` is the pure
  catalogue; `composables/useWebMCP.js` is the only Vue-aware part.
- **The catalogue mirrors `frontend/src/api.js` function for function.** That is
  what makes "anything the UI can do" checkable, and
  `frontend/test/webmcpTools.test.js` enforces it: add an API function without a
  tool and it fails. Exemptions live in that test with a reason.
- Tools act as the signed-in user with their cookie, so they inherit exactly the
  user's permissions. The registration is withdrawn on logout because the page
  is not reloaded in between.
- User-facing documentation is `docs/WEBMCP.md`; `cd desktop && npm run
  verify:webmcp` drives the whole path in a real browser with no backend.

## Telemetry

Most actions are emitted by the backend right after it does the work, which
makes them self-evidently true. A few happen entirely in the browser and are
*reported* by it instead — the local-AI features, and interface usage
(shortcuts, search, copies, the trajectory view).

- The allowlist in `entity.ClientReportableAction` is the security boundary for
  `POST /api/v1/telemetry`: only names in it may be reported, and the controller
  additionally checks the caller owns the workspace.
- Adding one means four places, or it reads as zero: the `Action` constant and
  its `String()`, the allowlist, the `model.ActionID*` constant (**append only**
  — the value is stored), and the mapping in `controller/telemetry`.
- `frontend/src/composables/useUiTelemetry.js` resolves the workspace from the
  route and **drops the report when there is none**, rather than guessing one.
  Interface counts are therefore actions-with-a-workspace-in-context.
- The route is rate limited per user. It was raised to 60/minute when interface
  usage was added; ordinary use passes the old ceiling of 10 easily, and being
  short there loses reports silently and starves the local-AI metrics that share
  the bucket.

## Commit convention

Include `Task: <taskID>` in the commit body for traceability.

## Coding Standards

- **API Naming**: All JSON fields in API requests and responses MUST use `camelCase` (e.g., `workspaceId`, `createdAt`). Never use `snake_case` in the API surface.
- **Backend Layers**: Follow view-entity-model separation; only `view` structs define the API schema. Avoid using repository directly from handlers; use controller methods instead.
