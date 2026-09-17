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

### Four traps that are invisible in source

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
- **The Linux app icon is two mechanisms, and packaging supplies one.** The
  single `icon:` in `electron-builder.yml` is the whole story on Windows (it is
  compiled into the `.exe`) and macOS (it is the `.icns` in the bundle), which
  is why a Linux-only icon bug is invisible on both. On Linux nothing tells a
  *running window* what to show, so every `BrowserWindow` is handed the icon
  explicitly via `desktop/src/main/app-icon.js` — and separately, the dock only
  shows it if the window can be matched to its installed launcher entry by app
  id, which is what `desktopName` in `desktop/package.json` and
  `linux.syncDesktopName` exist to make agree. Note that Linux is the one
  platform where electron-builder names things after `package.json`'s `name`
  rather than the product name, so the two must be `agentrq-desktop`, not
  `AgentRQ`. Changing either without the other silently returns the dock to a
  generic icon; `desktop/test/app-icon.test.js` is the check. The *installed*
  icon is a third thing again: electron-builder does not resize a PNG to build
  a freedesktop icon set, so `linux.icon` points at `desktop/resources/icons`,
  a committed set rendered from the app's SVG by `npm run icons`. Change the
  mark and re-run that, or a test fails on the recorded source hash.

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

The interface offers itself to a browser agent as WebMCP tools — 53 of them,
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

## Machines and the daemon (`daemon/`, `/machines`)

A machine is somebody's computer running `agentrqd`, enrolled against an
account. It holds a WebSocket to the backend, runs agents in real
pseudo-terminals, and streams them to the browser.

- **`daemon/` is a separate Go module**, wired in with a `replace` directive
  rather than a `go.work` (which is deliberately absent — it is not checked in,
  and builds must work without it). `daemon/wire` is the *only* definition of
  the frame format and is imported by the backend; there is no second copy to
  drift.
- **Two sockets, both on the stdlib `mux` in `app.go`, never on Fiber.** Fiber
  is mounted through an adaptor that synthesises a fasthttp context, and a
  WebSocket upgrade has to hijack a real connection, which a synthesised
  context does not have.
- **`terminalSocketUrl` and `serverOrigin` are the only absolute URLs in the
  frontend**, and they are a deliberate exception to the rule above rather than
  an oversight: Electron's custom-protocol handler forwards `/api` but does not
  intercept WebSockets, so a relative URL would resolve to `app://` and never
  open. Both ask the shell where the server is. Nothing else should.
- **Terminal input is exempt from the WebMCP parity rule on purpose**, with the
  reason written into the exemption in `frontend/test/webmcpTools.test.js`:
  raw keystroke access to a remote shell is a materially different grant from
  "anything the interface can do".
- **The attach is audited; the keystrokes are not.** A test types a password
  into a terminal and asserts it appears nowhere in the log.
- Machine and session events ride the **user's global** stream
  (`bus.Publish(0, userID, …)`), not a workspace's: a machine does not belong
  to a workspace, and the person watching the machines page may have none open.

### A reconnect is not a restart, and the sessions have to be told so

The daemon reconnects rather than exiting, and two things have to survive that
gap or reconnecting achieves nothing.

- **The agent must not be bound to the connection's context.** The pty layer
  kills the process whose context is done, and the context reaching
  `Supervisor.Start` is the daemon's socket to the backend — cancelled on every
  reconnect. Passing it straight through killed every agent on the machine
  whenever the backend restarted or the network blinked. `Start` therefore
  hands the process a `context.WithoutCancel` of it: what ends a session is
  `Kill`, the process itself, or `StopAll` at shutdown, never a dropped socket.
- **The pump belongs to the session, not to the socket.** It holds the screen,
  which has to survive the gap or the next viewer gets a blank terminal it can
  never get back, and it is blocked reading a pseudo-terminal nothing has
  closed — so a second pump would not replace the first, it would race it for
  every byte. On a lost connection the viewers are forgotten and the pumps
  stay; on the new one `streams.rebind` moves them across and repaints
  whatever somebody is still watching. The browser's own socket never dropped,
  so nobody is going to ask to attach again on its behalf.

Both were found by disabling a machine while an agent was running on it, which
closes the daemon's socket from the server's side — the cheapest way to make a
reconnect happen on demand.

### The agents go when the daemon goes

Stopping `agentrqd` stops every session it is running, explicitly, before the
connections close.

They would mostly go anyway — closing a pseudo-terminal hangs up on the process
using it — but "mostly" is not a design, and the failure it hides is total: an
agent that outlives its daemon is unreachable. Nothing lists it, nothing can
stop it, and the next daemon does not adopt it, so the panel shows an idle
machine while a process keeps working against the workspace with the
credential still sitting in its folder.

Two details in `cmdServe` are load-bearing and easy to undo:

- **The connections get their own context, not the signal's.** Sessions are
  killed and their exits reported while the sockets are still up; sharing the
  signal's context closes them first and the backend never hears what happened
  to the agents, which then linger in the panel as running.
- **The wait is bounded.** A process that ignores the hang-up must not hold the
  machine's shutdown open, so `StopAll` gives up after `shutdownGrace` and says
  how many it stopped.

### Self-update is the one place where getting it wrong is unrecoverable

- **A build with no release key refuses to update itself**, and says so. The key
  is a build-time `-ldflags` variable; empty is the default and the correct
  behaviour for every build that is not an official release. A verification step
  that silently passes when it has nothing to verify against is worse than none.
- **The manifest is signed as a whole** — version and platform table included,
  because those decide which file gets run — and every artefact's SHA-256 is
  mandatory and checked while downloading. There is no path to an artefact that
  skips the signature check.
- **The new binary is executed before anything is replaced.** After the swap the
  old process is gone and nothing can observe the new one failing; the previous
  binary is retained for `agentrqd rollback` and is deliberately *not* tidied up
  at startup.
- **Restoration is intent, not state**: new processes, new terminals, no
  scrollback. Restored sessions are marked as such so nobody wonders why their
  terminal is empty. The note that survives the restart carries no credential.

### A background without a foreground is a bug in one theme

Dark mode is a `.dark` class, and the main content area sets no colour of its
own — so text inherits the document default, which is black whatever the
theme. Any element that sets a background and no text colour is therefore
readable in exactly one of the two, and light mode is the one you are looking
at while writing it. `style.css` pairs them properly for `.md-body pre`; do the
same for anything new.

`color-scheme` is the other half, and it is not a Tailwind class. A class says
nothing to the parts of a page the *browser* draws — a `<select>`'s dropdown, a
caret, the autofill background, the default scrollbar — so without
`color-scheme: dark` on `.dark` those keep the light system palette on a dark
page, and no amount of styling from the page can reach them.

### Installation is answered in three places, and they must agree

The enrol command is useless on its own — it names a binary that is not there
yet — so the "Add machine" panel walks download → PATH → enrol → run, and the
steps come from `frontend/src/composables/useDaemonInstall.js` where they are
tested rather than from the template.

The same steps appear in `docs/DAEMON.md` (for somebody who has not downloaded
anything) and `daemon/packaging/INSTALL.md` (for somebody who has the archive
and not the page). Change one, change all three.

Two rules the tests enforce: **never a `curl … | sh` one-liner** — it asks
somebody to run unseen code on the machine they are about to grant command
access to — and **never `sudo` for running the daemon**, only for copying a
file into `/usr/local/bin`. The daemon refuses to run as root, and an install
guide that works around that has removed the only thing keeping an agent to
what its user can already do.

The detected platform picks which tab opens and nothing else: you are usually
setting up a machine other than the one you are browsing from.

### A viewer names no session, and must not

A browser holds base62 ids; the frame header wants a 64-bit number. It has no
way to produce one and does not need to — the backend decides which session an
attached socket may drive and overwrites whatever arrives, which is what stops
a browser typing into another session by changing a number. So viewer frames
carry a zero session and `wire.DecodeFromViewer` expects that.

Asking the browser for the id instead is what broke the terminal: `BigInt` of a
base62 string throws, inside a keystroke handler where nothing was watching, so
output kept arriving and the keyboard did nothing. Every unit test passed a
number; the application passes a string. Fixtures that do not match the shape
the caller actually uses are how a bug like that survives a full green suite.

### Both agent kinds read `.mcp.json`

It is easy to assume only claude-code does — the gateway takes its model and
agent on the command line, so it looks self-contained — and `make remote-agy`
reinforces that, because it happens to run from the repository root, which has
one. It does not: the gateway reads the workspace from the same file, and
without it prints "Could not find .mcp.json" and dies a second after starting.

So the backend mints an MCP token for both kinds and the daemon writes a config
for both.

### The terminal must never size itself

The fit addon reads the host element's box and sets the terminal's rows to
match. If that box is content-sized, fitting makes it taller, which makes the
box taller, which fits again — the terminal grows until it has pushed the page
off the bottom of the screen. That is what this page did.

So the host is a flex child with `min-h-0` all the way up, which gives it a
height that does not depend on its content, and the resize handler refuses to
act on a measurement that has not changed. Either alone is enough on a good
day; both is what makes it hard to reintroduce.

xterm's theme is also set explicitly, all sixteen colours. Agent output assumes
a dark background, and leaving the palette to a default that has never seen
this surface is where unreadable output comes from.

### A session is named by its workspace, and the name is filled in one place

A machine runs agents for several workspaces at once and most of them are the
same kind, so a session identified only by its kind is a row nobody can read —
three lines saying "claude-code" answer none of the questions somebody opened
the page with. `SessionView` therefore carries `WorkspaceName` beside the id,
and `nameWorkspaces` in the session controller fills it for every endpoint that
returns one. Add another and call it, or the page silently loses the name with
no error anywhere.

It is one query for the whole set, not one per row, and it is deliberately
best-effort: a lookup that fails leaves the sessions unnamed rather than
turning "what is running here" into an error page. The interface falls back to
the kind, which is what it showed before.

### Sessions are operational state, not history

A session row answers "what is running on this machine". When one finishes the
row is deleted — by the state report, by the reconcile on a daemon's hello, and
once at startup for rows written before that was true. The record of what
happened is the audit log; the rows are not it, and keeping them turns the
machine page into a list of everything that has ever run and the table into one
that grows forever.

The consequence to keep in mind: a failure is visible in the moment, over the
event stream, and not afterwards. If that needs to change, add a retention
window rather than keeping every row indefinitely.

### `ws: true` is what makes the terminal work in dev

`vite.config.js` proxies `/api` to the backend, and a proxy without `ws: true`
does not proxy upgrades — so every ordinary API call works and the terminal
socket is never proxied at all. The browser sits on "connecting" forever with
no error, because nothing ever answers the handshake.

### "Does this workspace already have an agent?" is two questions

The launch gate checks both, and needs both. `IsAgentConnected` answers "is an
agent talking to this workspace right now" — it says nothing about one that has
been started and has not finished connecting, and that window is seconds long,
easily enough to press the button twice and end up with two agents sharing one
`.mcp.json` and racing for the same tasks. `ActiveSessionForWorkspace` answers
that half from the database, and survives a backend restart into the bargain.
It was written for this and went uncalled until the interface gained a button.

The interface pre-empts what it can — `useAgentLaunch` works out every reason a
launch would be refused *before* anything is sent, because a form that fired
and reported whichever of the five refusals it hit would make somebody press
the button to find out whether they could press the button. It is never the
authority: two people can press at once, and only the server sees both.

The plan, with the decisions and who made them, is `docs/AGENTRQD_PLAN.md`.

## A turn ends at the usage footer, not at a reply

The ACP gateway gives a task one session and a session one turn at a time, and
chains a second delivery behind the first (`runTurnForTask` in the gateway).
A message sent while the agent is working is therefore queued, not read — so
the composer offers Stop instead of Send while a turn is running, and refuses
input until the turn ends.

Which makes "when does a turn end" load-bearing, and the obvious answer is
wrong. **A reply does not end a turn.** The server's own instructions tell
agents to report progress with `reply` every few steps, so a reply arriving is
usually the agent talking while it works; unlocking on one hands the composer
back mid-turn and queues the next message, which is the bug this exists to fix.

What ends a turn is the **usage footer**. The gateway flushes it from
`flushReply`, which runs when the ACP prompt resolves — "the last usage
snapshot of the turn", one per turn, including a turn cancelled by the Stop
button, since a cancel resolves the prompt too. That is what makes the composer
come back on its own after a stop.

The footer is only sent when the agent reported usage at all, so for a task
that has never seen one — and only then — a plain reply is taken as the end
instead. Worse, but the old behaviour rather than a composer that never
unlocks. The rule lives in `useAgentTurn.js`, with the cases as tests.

Everything is gated on `agentSupportsStop`: Claude Code speaking MCP directly
does not chain turns and cannot be stopped, so taking its Send away would leave
no way to say anything at all.

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
