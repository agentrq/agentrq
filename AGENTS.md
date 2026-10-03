# AgentRQ Codebase Notes

This file is loaded into the context of every agent working in this repository,
on every task, whatever it touches. It is kept short on purpose, and it is an
index: the detail lives in `docs/agents/`, one note per subsystem.

> **Open the note for the area you are about to change, before you change it.**
>
> The notes are not background reading, and they are not a summary of the code —
> the code is already there to read. Each one records the decisions that are
> *invisible* in the source: the change that looks like an obvious improvement
> and is a regression, usually on a platform nobody is running right now. They
> exist because somebody already made that mistake here.
>
> **Keep them short — a line or two.** State the rule and what goes wrong
> without it, then stop. Evidence, measurements and rejected alternatives
> belong in the pull request, which is read once; a note is read every time
> somebody opens the area. The same goes for code comments.

## Project layout

- `backend/` — Go backend (Fiber HTTP, GORM, MCP server)
- `frontend/` — Vue3 frontend, and the single source of UI for **both** the web and desktop builds
- `desktop/` — Electron shell; its renderer is built from `frontend/src`
- `daemon/` — `agentrqd`, which runs agents on somebody's machine. A **separate Go module**, wired in with a `replace` directive
- `cli/` — `cli/agentrq-ws` (npm `@agentrq/agentrq-ws`), a dependency-free command-line client for a workspace, covering every workspace MCP tool
- `plugins/` — harness plugins published from this repo (`plugins/deepseek-harness` → the `@agentrq/dsh-plugin-agentrq` bundle for DeepSeek Harness; `plugins/gemini`, the Gemini CLI extension, whose manifest must be the root `gemini-extension.json` — so a root `skills/`, `agents/`, `commands/`, `hooks/` or `policies/` folder would ship inside it), and `plugins/chrome`, the Chrome extension: a toolbar popup framing the server's own web app. It frames the hosted app rather than bundling `frontend/`, because on the extension's origin the `at` cookie never arrives, and the frame gets it only with host access to the server
- `qa/` — Playwright end-to-end sanity checks: the real web app against a real backend it starts itself, on ports 3911/5912 (see `qa/README.md`)
- `docs/agents/` — the notes below. The rest of `docs/` is user-facing documentation

## The notes

Each line says what the note will stop you getting wrong.

- **[Desktop app](docs/agents/desktop.md)** — `desktop/`, and *any* frontend
  change, since the desktop build renders the same Vue app. The `app://` proxy
  and why a cross-origin API call can never carry the auth cookie; how a profile
  keeps the name of the account it belongs to; why every main-process await
  needs a deadline; and five traps that leave no trace in source — Tailwind's
  scan root, the macOS title bar, macOS URL schemes, the macOS sandbox log
  line, and the Linux app icon.
- **[Machines and the daemon](docs/agents/machines-and-daemon.md)** — `daemon/`,
  the machines pages, terminals. Why a reconnect must not kill the agents and a
  shutdown must; why the browser names no session; why the terminal socket
  carries a ticket rather than the cookie every other call uses; why the
  terminal must never size itself; and the self-update rules, which are the one
  place here where getting it wrong cannot be undone.
- **[The MCP server, and tasks](docs/agents/mcp-and-tasks.md)** — adding or
  renaming a tool on the server is a change in four other places, and only one
  of them is guarded by a test that really compares the two.
  Also cron granularity, valid task statuses, how workspace memory is keyed,
  and why OAuth authorize must always show the consent page.
- **[CoreMCP, and the supervisor workspace](docs/agents/coremcp-and-supervisor.md)**
  — `backend/internal/handler/coremcp/`, the account-wide server every account's
  auto-created "supervisor" workspace talks to. A new tool there is a change in
  three places, all of them tested; a new resource or prompt is invisible
  to all of them and needs its own test instead.
- **[WebMCP](docs/agents/webmcp.md)** — the tool catalogue mirrors
  `frontend/src/api.js` function for function, and a test fails when it stops
  doing so. Read it before adding an API function.
- **[A turn ends at the usage footer](docs/agents/agent-turns.md)** — a reply
  does **not** end an agent's turn, and the turn's end is when the next of the
  composer's held messages is sent. Taking a reply for it posts one into the
  running turn, which is the bug the rule exists to fix.
- **[Site tools](docs/agents/site-tools.md)** — agents calling a shared
  website's WebMCP tools through the Chrome extension. Why it is native WebMCP
  and top frame only, why the nonce must be gone before page scripts run, and
  why calls never cross backend instances.
- **[Workspace forks](docs/agents/forks.md)** — a fork copies the parent's
  settings but reads its memory, skills and files from the parent. REST and
  CoreMCP share one merge, which stops the fork's agent first. A fork never
  starts on an agentrqd without the `fork` capability, or older than 0.9.3.
- **[Telemetry](docs/agents/telemetry.md)** — a new action is four places or it
  reads as zero, and the machine actions carry no workspace on purpose.
- **[Events](docs/agents/events.md)** — experimental. Named signals that let one
  workspace trigger tasks in another; publishing is agent-driven, not automatic.
- **[Storage and public links](docs/agents/storage.md)** — every attachment and
  skill file has a public link that is its only credential, predictable by
  design. Why the three checks on the unauthenticated `/storage/` route must
  stay, and why the storage dir is never served any other way.
- **[Skills](docs/agents/skills.md)** — why content is never in the database and
  the blob is written first, why an oversized file is refused rather than cut,
  why the importer builds its own GitHub URLs and keeps only referenced files,
  and why the UI never lists a skill's other files.

## Rules that hold everywhere

These are here rather than in a note because you can break them from outside the
area they belong to — while adding a route, or a single API call.

- **API naming**: All JSON fields in API requests and responses MUST use `camelCase` (e.g., `workspaceId`, `createdAt`). Never use `snake_case` in the API surface.
- **Backend layers**: Follow view-entity-model separation; only `view` structs define the API schema. Avoid using repository directly from handlers; use controller methods instead.
- **REST routes go on Fiber**, under `/api/v1`. The stdlib `mux` in `app.go` is
  only for SSE, pub-stats and the two WebSockets, and it **takes precedence for
  exact path matches** — so registering a Fiber route there as well does not
  duplicate it, it silently shadows the handler and hangs the request.
- **Never introduce an absolute API URL in the frontend.** It addresses the API
  with same-origin relative URLs and authenticates with the `at` cookie; an
  absolute URL works in the browser and breaks the desktop app, where it becomes
  a cross-origin request with no cookie. `terminalSocketUrl` and `serverOrigin`
  are the only exceptions, for a reason given in
  [the daemon note](docs/agents/machines-and-daemon.md).
- **A new dependency starts at its latest release**, and its range floor is
  that release (`^x.y.z`, not an older one). An older pin arrives with its
  advisories already open, and the floor lets a lockfile fall back to them.
- **Never relax backend CORS to accommodate the desktop app.** It does not need
  it — see [the desktop note](docs/agents/desktop.md) — and doing so widens the
  attack surface of every deployment.

## Running tests

```bash
cd backend && go test ./internal/...
```

Mock packages are **generated** (gitignored). Run `make mocks` before testing if they are missing. `mockgen` lives at `~/go/bin/mockgen`.

## Commit and pull request convention

Include `Task: <taskID>` in the commit body for traceability.

A pull request description carries the same five points, in this order:

- **What changed** — plain English, one or two sentences at most. Describe the
  behaviour, not the files; a reader who has never opened this repo should
  understand it.
- **Why changed** — the reason, concisely.
- **Test coverage report** — coverage of the lines this PR introduces, and the
  effect on the total.
- **Other reports** — optional, anything else worth recording.
- `Generated with [AgentRQ](https://agentrq.com) for TaskID: <taskID>`
